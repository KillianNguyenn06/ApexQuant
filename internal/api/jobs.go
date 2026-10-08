package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrCapacity       = errors.New("job capacity unavailable")
	ErrNotFound       = errors.New("job not found")
	ErrNotReady       = errors.New("result not ready")
	ErrResultTooLarge = errors.New("result exceeds replay budget")
)

type JobRunner func(context.Context, string, ValidatedRequest) (BacktestResultResponse, error)
type JobOptions struct {
	Workers, QueueSize, MaxRetained int
	Retention                       time.Duration
	MaxResultBytes, MaxStoredBytes  int64
}

func DefaultJobOptions() JobOptions {
	return JobOptions{
		Workers:        1,
		QueueSize:      4,
		MaxRetained:    20,
		Retention:      3 * time.Hour,
		MaxResultBytes: 64 << 20,
		MaxStoredBytes: 256 << 20,
	}
}

type job struct {
	id       string
	request  ValidatedRequest
	status   JobStatus
	failure  *APIError
	result   []byte
	finished time.Time
}
type JobManager struct {
	mu      sync.Mutex
	jobs    map[string]*job
	queue   chan *job
	options JobOptions
	runner  JobRunner
	ctx     context.Context
	cancel  context.CancelFunc
	closed  bool
	stored  int64
	done    chan struct{}
}

func NewJobManager(runner JobRunner, options JobOptions) (*JobManager, error) {
	if runner == nil || options.Workers < 1 || options.QueueSize < 1 || options.MaxRetained < 1 || options.Retention <= 0 || options.MaxResultBytes < 1 || options.MaxStoredBytes < options.MaxResultBytes {
		return nil, fmt.Errorf("invalid job configuration")
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &JobManager{
		jobs:    map[string]*job{},
		queue:   make(chan *job, options.QueueSize),
		options: options,
		runner:  runner,
		ctx:     ctx,
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	var wg sync.WaitGroup
	for i := 0; i < options.Workers; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); m.worker() }()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(min(options.Retention, time.Minute))
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				m.mu.Lock()
				m.prune(now)
				m.mu.Unlock()
			}
		}
	}()
	go func() { wg.Wait(); close(m.done) }()
	return m, nil
}
func (m *JobManager) Submit(request ValidatedRequest) (CreateBacktestResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return CreateBacktestResponse{}, ErrCapacity
	}
	m.prune(time.Now())
	request.Request.Allocations = append([]Allocation{}, request.Request.Allocations...)
	request.Allocations = append(request.Allocations[:0:0], request.Allocations...)
	j := &job{id: uuid.NewString(), request: request, status: StatusQueued}
	select {
	case m.queue <- j:
		m.jobs[j.id] = j
		return CreateBacktestResponse{ID: j.id, Status: StatusQueued}, nil
	default:
		return CreateBacktestResponse{}, ErrCapacity
	}
}
func (m *JobManager) Status(id string) (JobStatusResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune(time.Now())
	j, ok := m.jobs[id]
	if !ok {
		return JobStatusResponse{}, ErrNotFound
	}
	out := JobStatusResponse{
		ID:     id,
		Status: j.status,
	}
	if j.failure != nil {
		e := *j.failure
		out.Error = &e
	}
	return out, nil
}

// Result returns immutable serialized bytes. The caller owns its copy.
func (m *JobManager) Result(id string) ([]byte, error) {
	return m.resultLimited(id, 0)
}

// resultLimited checks the serialized size BEFORE allocating a caller-owned
// copy. Zero retains the ordinary result endpoint's existing behavior.
func (m *JobManager) resultLimited(id string, maxBytes int64) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune(time.Now())
	j, ok := m.jobs[id]
	if !ok {
		return nil, ErrNotFound
	}
	if j.status != StatusCompleted {
		return nil, ErrNotReady
	}
	if maxBytes > 0 && int64(len(j.result)) > maxBytes {
		return nil, ErrResultTooLarge
	}
	return append([]byte(nil), j.result...), nil
}
func (m *JobManager) worker() {
	for {
		select {
		case <-m.ctx.Done():
			return
		case j := <-m.queue:
			m.mu.Lock()
			if m.closed {
				m.mu.Unlock()
				return
			}
			j.status = StatusRunning
			m.mu.Unlock()
			data, failure := m.execute(j)
			m.mu.Lock()
			if m.closed {
				failure = &APIError{Code: "canceled", Message: "Server stopped before the job completed."}
				data = nil
			}
			j.finished = time.Now()
			j.request = ValidatedRequest{}
			if failure != nil {
				j.status = StatusFailed
				j.failure = failure
			} else {
				j.status = StatusCompleted
				j.result = data
				m.stored += int64(len(data))
			}
			m.prune(j.finished)
			m.mu.Unlock()
		}
	}
}
func (m *JobManager) execute(j *job) (data []byte, failure *APIError) {
	defer func() {
		if recover() != nil {
			data = nil
			failure = &APIError{Code: "internal_error", Message: "Backtest failed unexpectedly."}
		}
	}()
	response, err := m.runner(m.ctx, j.id, j.request)
	if err != nil {
		code := "internal_error"
		message := "Backtest could not be completed."
		var e *APIError
		if errors.As(err, &e) {
			switch e.Code {
			case "provider_error":
				code = e.Code
				message = "Market data could not be loaded."
			case "invalid_data":
				code = e.Code
				message = "Market data does not satisfy backtest requirements."
			case "invalid_result":
				code = e.Code
				message = "Backtest result cannot be represented safely."
			case "canceled":
				code = e.Code
				message = "Backtest was canceled."
			}
		}
		return nil, &APIError{Code: code, Message: message}
	}
	data, err = json.Marshal(response)
	if err != nil {
		return nil, &APIError{Code: "invalid_result", Message: "Backtest result cannot be represented safely."}
	}
	if int64(len(data)) > m.options.MaxResultBytes {
		return nil, &APIError{Code: "result_too_large", Message: "Result exceeds the configured storage limit; request a smaller range."}
	}
	return data, nil
}

// prune evicts only finished jobs, oldest first. Active jobs are never evicted.
func (m *JobManager) prune(now time.Time) {
	for id, j := range m.jobs {
		if !j.finished.IsZero() && now.Sub(j.finished) >= m.options.Retention {
			m.stored -= int64(len(j.result))
			delete(m.jobs, id)
		}
	}
	for {
		count := 0
		var oldest *job
		for _, j := range m.jobs {
			if !j.finished.IsZero() {
				count++
				if oldest == nil || j.finished.Before(oldest.finished) {
					oldest = j
				}
			}
		}
		if count <= m.options.MaxRetained && m.stored <= m.options.MaxStoredBytes {
			return
		}
		m.stored -= int64(len(oldest.result))
		delete(m.jobs, oldest.id)
	}
}

// Close stops admission, cancels provider requests and queued work, and waits
// for running computation. A timeout does not forcibly interrupt the engine.
func (m *JobManager) Close(ctx context.Context) error {
	m.mu.Lock()
	if !m.closed {
		m.closed = true
		m.cancel()
		for _, j := range m.jobs {
			if j.status == StatusQueued {
				j.status = StatusFailed
				j.failure = &APIError{Code: "canceled", Message: "Server stopped before the job started."}
				j.finished = time.Now()
				j.request = ValidatedRequest{}
			}
		}
		m.prune(time.Now())
	}
	m.mu.Unlock()
	select {
	case <-m.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
