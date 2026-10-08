package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"apexquant/internal/account"
	"apexquant/internal/backtest"
	"apexquant/internal/marketdata"
)

type tradingProvider struct {
	failure string
}

func (p tradingProvider) FetchBars(ctx context.Context, symbol string, start, end time.Time, key, secret string) ([]marketdata.BarTick, error) {
	if p.failure == "bars" {
		return nil, errors.New("private-provider-key")
	}
	start = time.Date(2025, 9, 5, 4, 0, 0, 0, time.UTC)
	prices := [][3]float64{{100, 100, 100}, {100, 90, 110}, {91, 110, 110}, {109, 109, 109}}
	if symbol == "MSFT" {
		prices = [][3]float64{{200, 200, 200}, {201, 201, 201}, {202, 202, 202}, {203, 203, 203}}
	}
	bars := make([]marketdata.BarTick, len(prices))
	for i, p := range prices {
		bars[i] = marketdata.BarTick{Symbol: symbol, Open: p[0], Close: p[1], VWAP: p[2], High: math.Max(p[0], p[1]), Low: math.Min(p[0], p[1]), Volume: 1000, Timestamp: start.AddDate(0, 0, i)}
	}
	if p.failure == "dates" && symbol == "MSFT" {
		bars[3].Timestamp = bars[3].Timestamp.AddDate(0, 0, 1)
	}
	return bars, nil
}
func (p tradingProvider) FetchRiskFreeRate(context.Context, string, time.Time) (float64, error) {
	if p.failure == "rate" {
		return 0, errors.New("private-provider-key")
	}
	return 1, nil
}
func tradingService(t *testing.T, p tradingProvider) (*backtest.Service, backtest.ServiceSettings) {
	t.Helper()
	settings := backtest.DefaultServiceSettings()
	settings.VolatilityWindow = 3
	settings.MonteCarlo.Volatility = .05
	settings.MonteCarlo.TimeHorizon = 1
	settings.MonteCarlo.NumPaths = 100
	settings.MonteCarlo.NumSteps = 10
	settings.MonteCarlo.Seed = 42
	s, err := backtest.NewService(p, backtest.Credentials{}, settings)
	if err != nil {
		t.Fatal(err)
	}
	return s, settings
}
func submitHTTP(t *testing.T, h *Handler) string {
	t.Helper()
	w := requestHTTP(h, "POST", "/api/backtests", requestBody, "application/json")
	if w.Code != 202 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var c CreateBacktestResponse
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	return c.ID
}
func TestHTTPTradingMatchesDirectEngine(t *testing.T) {
	provider := tradingProvider{}
	service, settings := tradingService(t, provider)
	metadata := DataInfo{Feed: "sip", Timeframe: "1Day", Adjustment: "raw"}
	h, m := httpTestHandler(t, ServiceRunner(service, metadata))
	id := submitHTTP(t, h)
	awaitStatus(t, m, id, StatusCompleted)
	w := requestHTTP(h, "GET", "/api/backtests/"+id+"/result", "", "")
	var got BacktestResultResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatal(w.Body.String())
	}
	req, err := DecodeCreateBacktestRequest(strings.NewReader(requestBody))
	if err != nil {
		t.Fatal(err)
	}
	v, err := ValidateCreateBacktestRequest(req, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	config := backtest.BacktestConfig{InitialAccount: account.Account{Cash: 50000, Equity: 50000, BuyingPower: 50000}, Allocations: v.Allocations, BarsBySymbol: map[string][]marketdata.BarTick{}, MonteCarloInput: settings.MonteCarlo, VolatilityWindow: 3, PeriodsPerYear: 252}
	config.MonteCarloInput.RiskFreeRate = 1
	for _, a := range v.Allocations {
		config.BarsBySymbol[a.Symbol], err = provider.FetchBars(context.Background(), a.Symbol, v.Start, v.End, "", "")
		if err != nil {
			t.Fatal(err)
		}
	}
	direct, err := backtest.RunBacktest(config)
	if err != nil {
		t.Fatal(err)
	}
	metadata.RiskFreeRate = 1
	want, err := NewResultResponse(id, v.Request, config, metadata, direct)
	if err != nil {
		t.Fatal(err)
	}
	// Random UUIDs identify orders, not simulation outcomes. Compare all other data.
	clearIDs := func(r *BacktestResultResponse) {
		for i := range r.Snapshots {
			for _, o := range []*Order{r.Snapshots[i].SubmittedOrder, r.Snapshots[i].FilledOrder} {
				if o != nil {
					o.ID = ""
				}
			}
		}
	}
	clearIDs(&got)
	clearIDs(&want)
	if !reflect.DeepEqual(got, want) {
		t.Fatal("HTTP result differs from direct engine result")
	}
	buys, sells := 0, 0
	for i, s := range got.Snapshots {
		if o := s.FilledOrder; o != nil {
			if o.Action == "buy" {
				buys++
				if i != 4 || *o.FilledPrice != 91 {
					t.Fatal("buy did not fill at next open")
				}
			} else if o.Action == "sell" {
				sells++
				if i != 6 || *o.FilledPrice != 109 {
					t.Fatal("sell did not fill at next open")
				}
			}
		}
		if i >= 6 && s.SubmittedOrder != nil {
			t.Fatal("final-bar submission")
		}
	}
	if buys != 1 || sells != 1 || got.FinalAccount.Cash <= 50000 || got.FinalAccount.Equity != got.FinalAccount.Cash {
		t.Fatal("trade/accounting regression")
	}
}
func TestHTTPProviderFailuresAndExpiry(t *testing.T) {
	for _, failure := range []string{"bars", "rate", "dates"} {
		t.Run(failure, func(t *testing.T) {
			service, _ := tradingService(t, tradingProvider{failure: failure})
			h, m := httpTestHandler(t, ServiceRunner(service, DataInfo{}))
			id := submitHTTP(t, h)
			awaitStatus(t, m, id, StatusFailed)
			w := requestHTTP(h, "GET", "/api/backtests/"+id, "", "")
			var s JobStatusResponse
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &s) != nil || s.Status != StatusFailed || s.Error == nil || strings.Contains(w.Body.String(), "private-provider-key") {
				t.Fatal("unsafe or missing failure status")
			}
			want := "provider_error"
			if failure == "dates" {
				want = "invalid_data"
			}
			if s.Error.Code != want {
				t.Fatalf("code %s want %s", s.Error.Code, want)
			}
			if w = requestHTTP(h, "GET", "/api/backtests/"+id+"/result", "", ""); w.Code != 409 {
				t.Fatal("failed job exposed result")
			}
			// Expire a finished record deterministically, without waiting 30 minutes.
			m.mu.Lock()
			m.jobs[id].finished = time.Now().Add(-m.options.Retention - time.Second)
			m.mu.Unlock()
			for _, suffix := range []string{"", "/result"} {
				if w = requestHTTP(h, "GET", "/api/backtests/"+id+suffix, "", ""); w.Code != 404 {
					t.Fatal("expired job still accessible")
				}
			}
		})
	}
}
func TestHTTPConcurrentAdmissionAndPolling(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	defer close(release)
	h, m := httpTestHandler(t, func(ctx context.Context, id string, v ValidatedRequest) (BacktestResultResponse, error) {
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-release:
			return BacktestResultResponse{ID: id}, nil
		case <-ctx.Done():
			return BacktestResultResponse{}, ctx.Err()
		}
	})
	first := submitHTTP(t, h)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	type outcome struct {
		code int
		id   string
	}
	outcomes := make(chan outcome, 24)
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := requestHTTP(h, "POST", "/api/backtests", requestBody, "application/json")
			var c CreateBacktestResponse
			_ = json.Unmarshal(w.Body.Bytes(), &c)
			outcomes <- outcome{w.Code, c.ID}
		}()
	}
	wg.Wait()
	close(outcomes)
	accepted := map[string]bool{first: true}
	for o := range outcomes {
		switch o.code {
		case 202:
			if o.id == "" || accepted[o.id] {
				t.Fatal("duplicate/missing job ID")
			}
			accepted[o.id] = true
		case 503:
		default:
			t.Fatalf("unexpected admission status %d", o.code)
		}
	}
	if len(accepted) > 1+m.options.QueueSize || len(accepted) < 2 {
		t.Fatalf("accepted %d", len(accepted))
	}
	statuses := make(chan int, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); statuses <- requestHTTP(h, "GET", "/api/backtests/"+first, "", "").Code }()
	}
	wg.Wait()
	close(statuses)
	for code := range statuses {
		if code != 200 && code != 503 {
			t.Fatalf("poll status %d", code)
		}
	}
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w := requestHTTP(h, "POST", "/api/backtests", requestBody, "application/json"); w.Code != 503 {
		t.Fatal("shutdown accepted job")
	}
}
func TestLimiterOutOfOrderTimestamps(t *testing.T) {
	b := newBucket(60, 1)
	now := time.Now()
	if !b.allow(now) {
		t.Fatal("initial token missing")
	}
	if b.allow(now.Add(-time.Second)) {
		t.Fatal("stale timestamp got token")
	}
	if b.allow(now) {
		t.Fatal("out-of-order request minted an extra token")
	}
	if !b.allow(now.Add(time.Second)) {
		t.Fatal("normal refill failed")
	}
}

func TestHTTPWireRoundTrip(t *testing.T) {
	service, _ := tradingService(t, tradingProvider{})
	h, m := httpTestHandler(t, ServiceRunner(service, DataInfo{Feed: "sip", Timeframe: "1Day", Adjustment: "raw"}))
	server := httptest.NewServer(h)
	defer server.Close()
	client := server.Client()
	client.Timeout = 3 * time.Second
	response, err := client.Post(server.URL+"/api/backtests", "application/json", strings.NewReader(requestBody))
	if err != nil {
		t.Fatal(err)
	}
	var c CreateBacktestResponse
	err = json.NewDecoder(response.Body).Decode(&c)
	response.Body.Close()
	if response.StatusCode != 202 || err != nil {
		t.Fatalf("create status=%d err=%v", response.StatusCode, err)
	}
	awaitStatus(t, m, c.ID, StatusCompleted)
	response, err = client.Get(server.URL + "/api/backtests/" + c.ID + "/result")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result BacktestResultResponse
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "application/json" || json.NewDecoder(response.Body).Decode(&result) != nil || len(result.Snapshots) != 8 {
		t.Fatal("HTTP transport contract failed")
	}
}

type delayedBody struct {
	entered, release chan struct{}
	once             sync.Once
}

func (b *delayedBody) Read([]byte) (int, error) {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return 0, io.EOF
}
func (b *delayedBody) Close() error { return nil }
func TestHTTPInFlightLimitAndRecovery(t *testing.T) {
	_, m := httpTestHandler(t, func(context.Context, string, ValidatedRequest) (BacktestResultResponse, error) {
		return BacktestResultResponse{}, nil
	})
	options := DefaultHandlerOptions()
	options.MaxInFlight = 1
	h, err := NewHandler(m, options)
	if err != nil {
		t.Fatal(err)
	}
	body := &delayedBody{entered: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	defer once.Do(func() { close(body.release) })
	r := httptest.NewRequest(http.MethodPost, "/api/backtests", nil)
	r.Body = body
	r.Header.Set("Content-Type", "application/json")
	done := make(chan struct{})
	go func() { h.ServeHTTP(httptest.NewRecorder(), r); close(done) }()
	select {
	case <-body.entered:
	case <-time.After(time.Second):
		t.Fatal("handler did not read body")
	}
	if w := requestHTTP(h, "GET", "/unknown", "", ""); w.Code != 503 {
		t.Fatal("in-flight limit not enforced")
	}
	once.Do(func() { close(body.release) })
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler did not release slot")
	}
	if w := requestHTTP(h, "GET", "/unknown", "", ""); w.Code != 404 {
		t.Fatal("slot not recovered")
	}
}
