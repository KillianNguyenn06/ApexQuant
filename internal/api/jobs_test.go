package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func testJobs(t *testing.T, runner JobRunner, options JobOptions) *JobManager {
	t.Helper()
	m, err := NewJobManager(runner, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := m.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return m
}
func awaitStatus(t *testing.T, m *JobManager, id string, want JobStatus) JobStatusResponse {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s, err := m.Status(id)
		if err != nil {
			t.Fatal(err)
		}
		if s.Status == want {
			return s
		}
		if s.Status == StatusFailed && want != StatusFailed {
			t.Fatalf("job failed: %+v", s.Error)
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("job never reached %s", want)
	return JobStatusResponse{}
}
func TestJobCapacityAndLifecycle(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	runner := func(ctx context.Context, id string, r ValidatedRequest) (BacktestResultResponse, error) {
		entered <- struct{}{}
		select {
		case <-release:
			return BacktestResultResponse{ID: id}, nil
		case <-ctx.Done():
			return BacktestResultResponse{}, ctx.Err()
		}
	}
	options := DefaultJobOptions()
	options.QueueSize = 1
	m := testJobs(t, runner, options)
	first, _ := m.Submit(ValidatedRequest{})
	<-entered
	second, err := m.Submit(ValidatedRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Submit(ValidatedRequest{}); !errors.Is(err, ErrCapacity) {
		t.Fatal("full queue accepted job")
	}
	awaitStatus(t, m, first.ID, StatusRunning)
	awaitStatus(t, m, second.ID, StatusQueued)
	if _, err = m.Result(first.ID); !errors.Is(err, ErrNotReady) {
		t.Fatal("running result available")
	}
	select {
	case <-entered:
		t.Fatal("more than one worker running")
	default:
	}
	once.Do(func() { close(release) })
	awaitStatus(t, m, first.ID, StatusCompleted)
	awaitStatus(t, m, second.ID, StatusCompleted)
	data, err := m.Result(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	data[0] = 'x'
	fresh, _ := m.Result(first.ID)
	if fresh[0] == 'x' {
		t.Fatal("result storage exposed mutable memory")
	}
}
func TestJobFailuresAndRecovery(t *testing.T) {
	calls := 0
	m := testJobs(t, func(ctx context.Context, id string, r ValidatedRequest) (BacktestResultResponse, error) {
		calls++
		switch calls {
		case 1:
			panic("secret")
		case 2:
			return BacktestResultResponse{}, &APIError{Code: "provider_error", Message: "secret"}
		default:
			return BacktestResultResponse{ID: id}, nil
		}
	}, DefaultJobOptions())
	for i := 0; i < 3; i++ {
		j, err := m.Submit(ValidatedRequest{})
		if err != nil {
			t.Fatal(err)
		}
		want := StatusFailed
		if i == 2 {
			want = StatusCompleted
		}
		s := awaitStatus(t, m, j.ID, want)
		if s.Error != nil && strings.Contains(s.Error.Message, "secret") {
			t.Fatal("leaked failure")
		}
	}
}
func TestJobRetentionAndSize(t *testing.T) {
	opts := DefaultJobOptions()
	opts.MaxRetained = 1
	m := testJobs(t, func(ctx context.Context, id string, r ValidatedRequest) (BacktestResultResponse, error) {
		return BacktestResultResponse{ID: id}, nil
	}, opts)
	first, _ := m.Submit(ValidatedRequest{})
	awaitStatus(t, m, first.ID, StatusCompleted)
	second, _ := m.Submit(ValidatedRequest{})
	awaitStatus(t, m, second.ID, StatusCompleted)
	if _, err := m.Status(first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("old completed job not evicted")
	}
	m.mu.Lock()
	m.prune(time.Now().Add(opts.Retention))
	m.mu.Unlock()
	if _, err := m.Status(second.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired job retained")
	}
	opts.MaxResultBytes = 1
	opts.MaxStoredBytes = 1
	small := testJobs(t, func(ctx context.Context, id string, r ValidatedRequest) (BacktestResultResponse, error) {
		return BacktestResultResponse{ID: id}, nil
	}, opts)
	j, _ := small.Submit(ValidatedRequest{})
	s := awaitStatus(t, small, j.ID, StatusFailed)
	if s.Error.Code != "result_too_large" {
		t.Fatal("oversize result not rejected")
	}
}
func TestJobShutdownAndConcurrentReads(t *testing.T) {
	entered := make(chan struct{})
	m := testJobs(t, func(ctx context.Context, id string, r ValidatedRequest) (BacktestResultResponse, error) {
		close(entered)
		<-ctx.Done()
		return BacktestResultResponse{}, ctx.Err()
	}, DefaultJobOptions())
	active, _ := m.Submit(ValidatedRequest{})
	<-entered
	queued, _ := m.Submit(ValidatedRequest{})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 20; n++ {
				_, _ = m.Status(active.ID)
				_, _ = m.Result(active.ID)
			}
		}()
	}
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	awaitStatus(t, m, queued.ID, StatusFailed)
	if _, err := m.Submit(ValidatedRequest{}); !errors.Is(err, ErrCapacity) {
		t.Fatal("closed manager accepted job")
	}
}

func TestShutdownDoesNotPretendToInterruptComputation(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	m := testJobs(t, func(context.Context, string, ValidatedRequest) (BacktestResultResponse, error) {
		close(entered)
		<-release
		return BacktestResultResponse{}, nil
	}, DefaultJobOptions())
	// Ensure a failing assertion still releases this intentionally non-cancelable runner.
	var once sync.Once
	defer once.Do(func() { close(release) })
	j, _ := m.Submit(ValidatedRequest{})
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := m.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close must wait for computation: %v", err)
	}
	s, _ := m.Status(j.ID)
	if s.Status != StatusRunning {
		t.Fatal("reported completion while computation still runs")
	}
	once.Do(func() { close(release) })
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	awaitStatus(t, m, j.ID, StatusFailed)
}

func TestResultByteBudgetEvictsOldest(t *testing.T) {
	response := BacktestResultResponse{ID: "fixed"}
	encoded, _ := json.Marshal(response)
	options := DefaultJobOptions()
	options.MaxResultBytes = int64(len(encoded))
	options.MaxStoredBytes = options.MaxResultBytes
	m := testJobs(t, func(context.Context, string, ValidatedRequest) (BacktestResultResponse, error) { return response, nil }, options)
	a, _ := m.Submit(ValidatedRequest{})
	awaitStatus(t, m, a.ID, StatusCompleted)
	b, _ := m.Submit(ValidatedRequest{})
	awaitStatus(t, m, b.ID, StatusCompleted)
	if _, err := m.Status(a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("byte budget did not evict oldest result")
	}
}
