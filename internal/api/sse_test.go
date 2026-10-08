package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A recorder that supports the same write-deadline capability as net/http's
// production writer. Actual deadline enforcement is tested over a real listener.
type replayRecorder struct {
	*httptest.ResponseRecorder
	deadlines  []time.Time
	writeError error
	flushError error
}

func newReplayRecorder() *replayRecorder {
	return &replayRecorder{ResponseRecorder: httptest.NewRecorder()}
}
func (w *replayRecorder) SetWriteDeadline(deadline time.Time) error {
	w.deadlines = append(w.deadlines, deadline)
	return nil
}
func (w *replayRecorder) Write(data []byte) (int, error) {
	if w.writeError != nil {
		return 0, w.writeError
	}
	return w.ResponseRecorder.Write(data)
}
func (w *replayRecorder) FlushError() error {
	if w.flushError != nil {
		return w.flushError
	}
	w.ResponseRecorder.Flush()
	return nil
}

func readyReplay(t *testing.T) (*Handler, *JobManager, string) {
	t.Helper()
	service, _ := tradingService(t, tradingProvider{})
	h, m := httpTestHandler(t, ServiceRunner(service, DataInfo{Feed: "sip", Timeframe: "1Day", Adjustment: "raw"}))
	id := submitHTTP(t, h)
	awaitStatus(t, m, id, StatusCompleted)
	return h, m, id
}

// Parse the wire format independently of production formatting. Reject missing
// fields and incomplete final frames rather than quietly dropping an event.
func readSSE(reader io.Reader) ([]ReplayEvent, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 32<<20)
	var events []ReplayEvent
	var event ReplayEvent
	fields := 0
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if fields != 3 || event.ID < 1 || !json.Valid(event.Data) {
				return nil, errors.New("invalid SSE frame")
			}
			events = append(events, event)
			event, fields = ReplayEvent{}, 0
			continue
		}
		key, value, ok := strings.Cut(line, ": ")
		if !ok {
			return nil, errors.New("invalid SSE field")
		}
		switch key {
		case "id":
			id, err := strconv.Atoi(value)
			if err != nil {
				return nil, err
			}
			event.ID = id
		case "event":
			event.Type = ReplayEventType(value)
		case "data":
			event.Data = json.RawMessage(value)
		default:
			return nil, errors.New("unexpected SSE field")
		}
		fields++
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if fields != 0 {
		return nil, errors.New("incomplete SSE frame")
	}
	return events, nil
}

func TestSSEWireReplayAndResume(t *testing.T) {
	service, _ := tradingService(t, tradingProvider{})
	var runs atomic.Int32
	runner := ServiceRunner(service, DataInfo{Feed: "sip", Timeframe: "1Day", Adjustment: "raw"})
	h, m := httpTestHandler(t, func(ctx context.Context, id string, r ValidatedRequest) (BacktestResultResponse, error) {
		runs.Add(1)
		return runner(ctx, id, r)
	})
	server := httptest.NewUnstartedServer(h)
	// Replay must survive a server-wide deadline shorter than a playback wait.
	server.Config.WriteTimeout = 30 * time.Millisecond
	server.Start()
	defer server.Close()
	client := server.Client()
	client.Timeout = 3 * time.Second
	response, err := client.Post(server.URL+"/api/backtests", "application/json", strings.NewReader(requestBody))
	if err != nil {
		t.Fatal(err)
	}
	var job CreateBacktestResponse
	err = json.NewDecoder(response.Body).Decode(&job)
	response.Body.Close()
	if err != nil || response.StatusCode != 202 {
		t.Fatalf("admission: %v status=%d", err, response.StatusCode)
	}
	awaitStatus(t, m, job.ID, StatusCompleted)
	encoded, _ := m.Result(job.ID)
	var result BacktestResultResponse
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	path := server.URL + "/api/backtests/" + job.ID + "/replay?speed=20"
	response, err = client.Get(path)
	if err != nil {
		t.Fatal(err)
	}
	events, err := readSSE(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || response.Header.Get("Content-Type") != "text/event-stream" || response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("stream response: %v status=%d", err, response.StatusCode)
	}
	want := []ReplayEventType{ReplayStart, ReplaySnapshot, ReplaySnapshot, ReplayPortfolio, ReplaySnapshot, ReplaySnapshot, ReplayPortfolio, ReplaySnapshot, ReplaySnapshot, ReplayPortfolio, ReplaySnapshot, ReplaySnapshot, ReplayPortfolio, ReplayComplete}
	if len(events) != len(want) {
		t.Fatalf("received %d events, want %d", len(events), len(want))
	}
	snapshotIndex := 0
	for i, event := range events {
		if event.ID != i+1 || event.Type != want[i] {
			t.Fatalf("event %d out of order", i)
		}
		if event.Type == ReplaySnapshot {
			data := decodeReplay[ReplaySnapshotData](t, event)
			wantSymbol := []string{"AAPL", "MSFT"}[snapshotIndex%2]
			wantDate := []string{"2025-09-05", "2025-09-06", "2025-09-07", "2025-09-08"}[snapshotIndex/2]
			if data.SnapshotIndex != snapshotIndex || data.Snapshot.Bar.Symbol != wantSymbol || data.Snapshot.Timestamp.Format(time.DateOnly) != wantDate || !reflect.DeepEqual(data.Snapshot, result.Snapshots[snapshotIndex]) {
				t.Fatal("wire snapshot differs from expected sequence or engine result")
			}
			snapshotIndex++
		}
	}
	complete := decodeReplay[ReplayCompleteData](t, events[len(events)-1])
	if complete.FinalAccount != result.FinalAccount || !reflect.DeepEqual(complete.FinalPositions, result.FinalPositions) {
		t.Fatal("completion differs from engine result")
	}
	// Resume midway through a day; every cursor includes all remaining events.
	for _, cursor := range []int{1, 2, 4, 13, 14} {
		request, _ := http.NewRequest("GET", path, nil)
		request.Header.Set("Last-Event-ID", strconv.Itoa(cursor))
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		if cursor == 14 {
			if response.StatusCode != 204 {
				t.Fatal("completed cursor did not stop reconnect")
			}
		} else {
			resumed, err := readSSE(response.Body)
			if err != nil || !reflect.DeepEqual(resumed, events[cursor:]) {
				t.Fatalf("resume cursor %d: %v", cursor, err)
			}
		}
		response.Body.Close()
	}
	if runs.Load() != 1 {
		t.Fatal("replay reran calculation")
	}
}

func TestSSEPacingAndWriteFailures(t *testing.T) {
	h, _, id := readyReplay(t)
	var intervals []time.Duration
	h.waitReplay = func(ctx context.Context, interval time.Duration) error {
		intervals = append(intervals, interval)
		return ctx.Err()
	}
	w := newReplayRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/backtests/"+id+"/replay?speed=2", nil))
	if !reflect.DeepEqual(intervals, []time.Duration{500 * time.Millisecond, 500 * time.Millisecond, 500 * time.Millisecond}) {
		t.Fatalf("wrong daily pacing: %v", intervals)
	}
	if w.Code != 200 || !w.Flushed || len(h.replays) != 0 {
		t.Fatal("stream did not flush or release capacity")
	}
	for _, deadline := range w.deadlines {
		if !deadline.IsZero() && time.Until(deadline) > h.options.Replay.WriteTimeout {
			t.Fatal("unbounded write deadline")
		}
	}
	for _, failure := range []string{"write", "flush"} {
		w := newReplayRecorder()
		if failure == "write" {
			w.writeError = io.ErrClosedPipe
		} else {
			w.flushError = io.ErrClosedPipe
		}
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/backtests/"+id+"/replay", nil))
		if len(h.replays) != 0 || strings.Contains(w.Body.String(), "event: complete") {
			t.Fatal("failed stream leaked capacity or claimed completion")
		}
	}
}

func TestSSERequestValidation(t *testing.T) {
	h, _, id := readyReplay(t)
	path := "/api/backtests/" + id + "/replay"
	for _, tc := range []struct{ query, cursor string }{
		{"?speed=0", ""}, {"?speed=NaN", ""}, {"?speed=21", ""}, {"?speed=", ""}, {"?speed=1&speed=2", ""}, {"?other=1", ""}, {"?speed=%zz", ""},
		{"", "-1"}, {"", "+1"}, {"", "abc"}, {"", "15"}, {"", "99999999999999999999999"},
	} {
		request := httptest.NewRequest("GET", path+tc.query, nil)
		if tc.cursor != "" {
			request.Header.Set("Last-Event-ID", tc.cursor)
		}
		w := newReplayRecorder()
		h.ServeHTTP(w, request)
		if w.Code != 400 || w.Header().Get("Content-Type") != "application/json" || len(h.replays) != 0 {
			t.Fatalf("validation %q %q: %d", tc.query, tc.cursor, w.Code)
		}
	}
	request := httptest.NewRequest("GET", path, nil)
	request.Header.Add("Last-Event-ID", "1")
	request.Header.Add("Last-Event-ID", "2")
	w := newReplayRecorder()
	h.ServeHTTP(w, request)
	if w.Code != 400 {
		t.Fatal("duplicate cursor accepted")
	}
	if w := requestHTTP(h, "POST", path, "", ""); w.Code != 405 || w.Header().Get("Allow") != "GET" {
		t.Fatal("replay method validation missing")
	}
	if w := requestHTTP(h, "GET", path, "", ""); w.Code != 500 {
		t.Fatal("unbounded response writer accepted")
	}
}

func TestSSEUnavailableAndBudgets(t *testing.T) {
	h, m, id := readyReplay(t)
	for _, status := range []JobStatus{StatusQueued, StatusRunning, StatusFailed} {
		m.mu.Lock()
		m.jobs[id].status = status
		m.mu.Unlock()
		if w := requestHTTP(h, "GET", "/api/backtests/"+id+"/replay", "", ""); w.Code != 409 {
			t.Fatalf("status %s got %d", status, w.Code)
		}
	}
	m.mu.Lock()
	m.jobs[id].status = StatusCompleted
	m.mu.Unlock()
	h.options.Replay.MaxBytes = 1
	if w := requestHTTP(h, "GET", "/api/backtests/"+id+"/replay", "", ""); w.Code != 413 {
		t.Fatal("source budget ignored")
	}
	h.options.Replay.MaxBytes = DefaultReplayOptions().MaxBytes
	h.options.MaxSnapshots = 1
	if w := requestHTTP(h, "GET", "/api/backtests/"+id+"/replay", "", ""); w.Code != 413 {
		t.Fatal("snapshot budget ignored")
	}
	h.options.MaxSnapshots = DefaultHandlerOptions().MaxSnapshots
	encoded, _ := m.Result(id)
	// Source fits; the larger timeline (extra portfolio data) does not.
	h.options.Replay.MaxBytes = int64(len(encoded))
	if w := requestHTTP(h, "GET", "/api/backtests/"+id+"/replay", "", ""); w.Code != 413 {
		t.Fatal("timeline budget ignored")
	}
	h.options.Replay.MaxBytes = DefaultReplayOptions().MaxBytes
	m.mu.Lock()
	m.jobs[id].result = []byte(`{"id":"wrong-job"}`)
	m.mu.Unlock()
	if w := requestHTTP(h, "GET", "/api/backtests/"+id+"/replay", "", ""); w.Code != 500 {
		t.Fatal("invalid result accepted")
	}
	m.mu.Lock()
	m.jobs[id].finished = time.Now().Add(-2 * m.options.Retention)
	m.mu.Unlock()
	if w := requestHTTP(h, "GET", "/api/backtests/"+id+"/replay", "", ""); w.Code != 404 {
		t.Fatal("expired result accepted")
	}
	if w := requestHTTP(h, "GET", "/api/backtests/missing/replay", "", ""); w.Code != 404 {
		t.Fatal("missing result accepted")
	}
}

func waitTestSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for stream transition")
	}
}

func TestSSECapacityCancellationAndExpiryOwnership(t *testing.T) {
	h, m, id := readyReplay(t)
	h.replays = make(chan struct{}, 1)
	h.inflight = make(chan struct{}, 1)
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	h.waitReplay = func(ctx context.Context, interval time.Duration) error {
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return nil
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := newReplayRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/backtests/"+id+"/replay", nil).WithContext(ctx))
	}()
	waitTestSignal(t, entered)
	if response := requestHTTP(h, "GET", "/api/backtests/"+id+"/replay", "", ""); response.Code != 503 {
		t.Fatal("connection cap ignored")
	}
	if response := requestHTTP(h, "GET", "/api/backtests/"+id, "", ""); response.Code != 200 {
		t.Fatal("stream occupied normal handler slots")
	}
	cancel()
	waitTestSignal(t, done)
	if len(h.replays) != 0 || strings.Contains(w.Body.String(), "event: complete") {
		t.Fatal("disconnect leaked capacity or claimed completion")
	}
	// A new stream owns its copy even after the job is removed during playback.
	w = newReplayRecorder()
	done = make(chan struct{})
	go func() {
		defer close(done)
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/backtests/"+id+"/replay", nil))
	}()
	waitTestSignal(t, entered)
	m.mu.Lock()
	m.jobs[id].finished = time.Now().Add(-2 * m.options.Retention)
	m.prune(time.Now())
	m.mu.Unlock()
	close(release)
	waitTestSignal(t, done)
	if !strings.Contains(w.Body.String(), "event: complete") {
		t.Fatal("job eviction interrupted owned replay")
	}
	if response := requestHTTP(h, "GET", "/api/backtests/"+id+"/replay", "", ""); response.Code != 404 {
		t.Fatal("expired replay remained available for new connections")
	}
}

func TestSSEShutdownAndDuration(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(fmt.Sprint(shutdown), func(t *testing.T) {
			h, m, id := readyReplay(t)
			if !shutdown {
				h.options.Replay.MaxDuration = 20 * time.Millisecond
			}
			entered := make(chan struct{})
			h.waitReplay = func(ctx context.Context, _ time.Duration) error { close(entered); <-ctx.Done(); return ctx.Err() }
			done := make(chan struct{})
			w := newReplayRecorder()
			go func() {
				defer close(done)
				h.ServeHTTP(w, httptest.NewRequest("GET", "/api/backtests/"+id+"/replay", nil))
			}()
			waitTestSignal(t, entered)
			if shutdown {
				if err := m.Close(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			waitTestSignal(t, done)
			if len(h.replays) != 0 || strings.Contains(w.Body.String(), "event: complete") {
				t.Fatal("cancellation leaked or claimed completion")
			}
			if shutdown {
				if w := requestHTTP(h, "GET", "/api/backtests/"+id+"/replay", "", ""); w.Code != 503 {
					t.Fatal("shutdown admitted stream")
				}
			}
		})
	}
}

func TestSSEWireDisconnectReleasesConnection(t *testing.T) {
	h, _, id := readyReplay(t)
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		h.ServeHTTP(w, r)
	}))
	defer server.Close()
	client := server.Client()
	client.Timeout = 3 * time.Second
	response, err := client.Get(server.URL + "/api/backtests/" + id + "/replay?speed=0.25")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 {
		response.Body.Close()
		t.Fatalf("stream status %d", response.StatusCode)
	}
	// Headers/first event arrive before the four-second playback wait finishes.
	reader := bufio.NewReader(response.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			response.Body.Close()
			t.Fatal(err)
		}
		if line == "\n" {
			break
		}
	}
	response.Body.Close()
	waitTestSignal(t, done)
	if len(h.replays) != 0 {
		t.Fatal("disconnected HTTP client retained replay capacity")
	}
}

func TestSSESlowReaderWriteDeadline(t *testing.T) {
	fixture, _ := replayFixture(t)
	// A large but budget-compliant event makes write backpressure deterministic
	// with a small TCP send buffer. The client deliberately reads no body bytes.
	fixture.Snapshots[0].SubmittedOrder = &Order{ID: strings.Repeat("x", 2<<20), Symbol: "AAPL", Action: "buy", Status: "submitted"}
	h, m := httpTestHandler(t, func(ctx context.Context, id string, r ValidatedRequest) (BacktestResultResponse, error) {
		fixture.ID = id
		return fixture, nil
	})
	id := submitHTTP(t, h)
	awaitStatus(t, m, id, StatusCompleted)
	h.options.Replay.WriteTimeout = 50 * time.Millisecond
	done := make(chan struct{})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { defer close(done); h.ServeHTTP(w, r) }))
	server.Config.ConnContext = func(ctx context.Context, conn net.Conn) context.Context {
		if tcp, ok := conn.(*net.TCPConn); ok {
			if err := tcp.SetWriteBuffer(1024); err != nil {
				t.Error(err)
			}
		}
		return ctx
	}
	server.Start()
	defer server.Close()
	conn, err := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if tcp, ok := conn.(*net.TCPConn); ok {
		if err := tcp.SetReadBuffer(1024); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fmt.Fprintf(conn, "GET /api/backtests/%s/replay HTTP/1.1\r\nHost: localhost\r\n\r\n", id); err != nil {
		t.Fatal(err)
	}
	waitTestSignal(t, done)
	if len(h.replays) != 0 {
		t.Fatal("slow client retained replay capacity")
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	wire, _ := io.ReadAll(conn)
	if !strings.Contains(string(wire), "event: start") || strings.Contains(string(wire), "event: complete") {
		t.Fatal("slow stream did not start, or falsely completed")
	}
}
