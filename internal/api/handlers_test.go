package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"apexquant/internal/backtest"
	"apexquant/internal/marketdata"
)

const requestBody = `{"initial_capital":50000,"allocations":[{"symbol":"AAPL","percent":50},{"symbol":"MSFT","percent":50}],"start_date":"2025-09-05","end_date":"2026-09-04"}`

func httpTestHandler(t *testing.T, runner JobRunner) (*Handler, *JobManager) {
	t.Helper()
	m := testJobs(t, runner, DefaultJobOptions())
	opts := DefaultHandlerOptions()
	opts.RequestBurst = 1000
	opts.SubmissionBurst = 1000
	h, err := NewHandler(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	return h, m
}
func requestHTTP(h http.Handler, method, path, body, contentType string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

type httpProvider struct{}

func (httpProvider) FetchBars(ctx context.Context, symbol string, start, end time.Time, key, secret string) ([]marketdata.BarTick, error) {
	bars := []marketdata.BarTick{}
	for i := 0; i < 3; i++ {
		bars = append(bars, marketdata.BarTick{Symbol: symbol, Timestamp: start.AddDate(0, 0, i), Open: 100, High: 100, Low: 100, Close: 100, VWAP: 100, Volume: 100})
	}
	return bars, nil
}
func (httpProvider) FetchRiskFreeRate(context.Context, string, time.Time) (float64, error) {
	return .04, nil
}
func TestHTTPBacktestLifecycle(t *testing.T) {
	settings := backtest.DefaultServiceSettings()
	settings.MonteCarlo.Seed = 42
	settings.MonteCarlo.NumPaths = 10
	service, err := backtest.NewService(httpProvider{}, backtest.Credentials{}, settings)
	if err != nil {
		t.Fatal(err)
	}
	h, m := httpTestHandler(t, ServiceRunner(service, DataInfo{Feed: "sip", Timeframe: "1Day", Adjustment: "raw"}))
	// Real handler, service, and engine; only external market data is replaced.
	w := requestHTTP(h, "POST", "/api/backtests", requestBody, "application/json")
	if w.Code != 202 {
		t.Fatalf("create %d %s", w.Code, w.Body.String())
	}
	var created CreateBacktestResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || w.Header().Get("Location") != "/api/backtests/"+created.ID {
		t.Fatal("missing job location")
	}
	awaitStatus(t, m, created.ID, StatusCompleted)
	w = requestHTTP(h, "GET", "/api/backtests/"+created.ID, "", "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = requestHTTP(h, "GET", "/api/backtests/"+created.ID+"/result", "", "")
	var result BacktestResultResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatal(w.Body.String())
	}
	if len(result.Snapshots) != 6 || result.FinalAccount.Cash != 50000 || result.Settings.Seed != 42 || result.Data.RiskFreeRate != .04 {
		t.Fatal("incorrect pipeline output")
	}
}
func TestHTTPValidationAndLimits(t *testing.T) {
	h, _ := httpTestHandler(t, func(context.Context, string, ValidatedRequest) (BacktestResultResponse, error) {
		return BacktestResultResponse{}, nil
	})
	for _, tc := range []struct {
		method, path, body, media string
		status                    int
	}{
		{"POST", "/api/backtests", requestBody, "text/plain", 415},
		{"POST", "/api/backtests", "{", "application/json", 400},
		{"POST", "/api/backtests", strings.Repeat("x", int(h.options.MaxBodyBytes)+1), "application/json", 413},
		{"POST", "/api/backtests", strings.Replace(requestBody, "2025-09-05", "2020-01-01", 1), "application/json", 400},
		{"POST", "/api/backtests", strings.Replace(requestBody, "MSFT", "aapl", 1), "application/json", 400},
		{"GET", "/api/backtests/unknown", "", "", 404},
		{"GET", "/api/backtests/unknown/result", "", "", 404},
		{"GET", "/api/backtests", "", "", 405},
		{"POST", "/api/backtests/unknown", "", "", 405},
		{"GET", "/unknown", "", "", 404},
	} {
		w := requestHTTP(h, tc.method, tc.path, tc.body, tc.media)
		if w.Code != tc.status {
			t.Fatalf("%s %s got %d want %d: %s", tc.method, tc.path, w.Code, tc.status, w.Body.String())
		}
	}
	h.options.MaxSnapshots = 2
	if w := requestHTTP(h, "POST", "/api/backtests", requestBody, "application/json"); w.Code != 400 {
		t.Fatal("snapshot budget ignored")
	}
	h.requests = newBucket(1, 1)
	requestHTTP(h, "GET", "/unknown", "", "")
	if w := requestHTTP(h, "GET", "/unknown", "", ""); w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatal("request limiter failed")
	}
	h.requests = newBucket(1000, 1000)
	h.submissions = newBucket(1, 1)
	requestHTTP(h, "POST", "/api/backtests", "{}", "application/json")
	if w := requestHTTP(h, "POST", "/api/backtests", "{}", "application/json"); w.Code != 429 {
		t.Fatal("submission limiter failed")
	}
}
func TestHTTPQueueAndResultUnavailable(t *testing.T) {
	entered := make(chan struct{}, 1)
	h, m := httpTestHandler(t, func(ctx context.Context, id string, r ValidatedRequest) (BacktestResultResponse, error) {
		entered <- struct{}{}
		<-ctx.Done()
		return BacktestResultResponse{}, ctx.Err()
	})
	first := requestHTTP(h, "POST", "/api/backtests", requestBody, "application/json")
	var created CreateBacktestResponse
	_ = json.Unmarshal(first.Body.Bytes(), &created)
	<-entered
	w := requestHTTP(h, "GET", "/api/backtests/"+created.ID+"/result", "", "")
	if w.Code != 409 {
		t.Fatal("premature result")
	}
	for i := 0; i < m.options.QueueSize; i++ {
		w = requestHTTP(h, "POST", "/api/backtests", requestBody, "application/json")
		if w.Code != 202 {
			t.Fatal(w.Body.String())
		}
	}
	if w = requestHTTP(h, "POST", "/api/backtests", requestBody, "application/json"); w.Code != 503 {
		t.Fatal("full queue should return 503")
	}
}
func TestBucketRefills(t *testing.T) {
	b := newBucket(60, 1)
	now := time.Now()
	if !b.allow(now) || b.allow(now) || !b.allow(now.Add(time.Second)) {
		t.Fatal("token bucket refill incorrect")
	}
}
