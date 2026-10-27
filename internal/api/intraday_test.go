package api

import (
	"apexquant/internal/account"
	"apexquant/internal/backtest"
	"apexquant/internal/marketdata"
	"apexquant/internal/session"
	"apexquant/internal/simulation"
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestMinuteEngineMappingAndReplay(t *testing.T) {
	start := time.Date(2026, 10, 5, 13, 30, 0, 0, time.UTC)
	var bars []marketdata.BarTick
	for i, p := range []float64{100, 102, 98, 99, 100} {
		bars = append(bars, marketdata.BarTick{Symbol: "AAPL", Timestamp: start.Add(time.Duration(i) * time.Minute), Open: p, High: p, Low: p, Close: p, VWAP: p, Volume: 100})
	}
	bars[3].Low = 90
	bars[3].High = 110
	c := backtest.BacktestConfig{Timeframe: "1Min", InitialAccount: account.Account{Cash: 50000, Equity: 50000, BuyingPower: 50000}, Allocations: []session.PortfolioAllocation{{Symbol: "AAPL", Weight: 1}}, BarsBySymbol: map[string][]marketdata.BarTick{"AAPL": bars}, VolatilityWindow: 20, PeriodsPerYear: 252, MonteCarloInput: simulation.MonteCarlo{RiskFreeRate: 2, TimeHorizon: 5.0 / 252, NumPaths: 10, NumSteps: 252, Seed: 42}}
	result, err := backtest.RunBacktest(c)
	if err != nil {
		t.Fatal(err)
	}
	request := CreateBacktestRequest{Timeframe: "1Min", InitialCapital: 50000, StartDate: "2026-10-05", EndDate: "2026-10-05", Allocations: []Allocation{{Symbol: "AAPL", Percent: 100}}}
	mapped, err := NewResultResponse("minute", request, c, DataInfo{Timeframe: "1Min"}, result)
	if err != nil {
		t.Fatal(err)
	}
	if len(mapped.Snapshots[3].FilledOrders) != 2 || mapped.Snapshots[2].Risk == nil || mapped.Snapshots[2].Indicator.EMA == nil || !mapped.Snapshots[2].Signal.CreatedAt.Equal(start.Add(3*time.Minute)) {
		t.Fatal("minute diagnostics lost during mapping")
	}
	timeline, err := NewReplayTimeline(mapped)
	if err != nil {
		t.Fatal(err)
	}
	if timeline.Days() != 5 || timeline.Len() != 12 {
		t.Fatalf("minute groups: %d %d", timeline.Days(), timeline.Len())
	}
	event, _ := timeline.Event(2)
	var frame ReplayPortfolioData
	if err := json.Unmarshal(event.Data, &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Date != "2026-10-05T13:30:00Z" {
		t.Fatal(frame.Date)
	}
	validated, err := ValidateCreateBacktestRequest(request, start.AddDate(0, 0, 2))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateRequestLimits(validated, 365, 390); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRequestLimits(validated, 365, 389); err == nil {
		t.Fatal("minute workload not bounded")
	}
}

// Existing daily fake remains usable; the minute-capable provider is optional.
type minuteProvider struct {
	tradingProvider
	timeframe string
}

func (p *minuteProvider) FetchBarsTimeframe(ctx context.Context, symbol string, start, end time.Time, key, secret, timeframe string) ([]marketdata.BarTick, error) {
	p.timeframe = timeframe
	var bars []marketdata.BarTick
	for i := 0; i < 5; i++ {
		bars = append(bars, marketdata.BarTick{Symbol: symbol, Timestamp: start.Add(9*time.Hour + 30*time.Minute + time.Duration(i)*time.Minute), Open: 100, High: 100, Low: 100, Close: 100, VWAP: 100, Volume: 100})
	}
	return bars, nil
}

func TestServiceRunnerRejectsDisabledMinuteExecutionBeforeFetching(t *testing.T) {
	p := &minuteProvider{}
	service, err := backtest.NewService(p, backtest.Credentials{}, backtest.DefaultServiceSettings())
	if err != nil {
		t.Fatal(err)
	}
	request := validRequest()
	request.Timeframe = "1Min"
	validated, err := ValidateCreateBacktestRequest(request, testNow())
	if err != nil {
		t.Fatal(err)
	}
	_, err = ServiceRunner(service, DataInfo{Timeframe: "1Day"})(context.Background(), "minute", validated)
	apiError, ok := err.(*APIError)
	if !ok || apiError.Code != "minute_execution_disabled" || p.timeframe != "" {
		t.Fatalf("minute request not blocked: %v provider=%s", err, p.timeframe)
	}
}

func TestHTTPRejectsMinuteExecutionBeforeQueue(t *testing.T) {
	h, _ := httpTestHandler(t, func(context.Context, string, ValidatedRequest) (BacktestResultResponse, error) {
		t.Error("disabled minute request reached runner")
		return BacktestResultResponse{}, nil
	})
	request := validRequest()
	request.Timeframe = "1Min"
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	response := requestHTTP(h, "POST", "/api/backtests", string(body), "application/json")
	var failure ErrorResponse
	if response.Code != 400 || json.Unmarshal(response.Body.Bytes(), &failure) != nil || failure.Error.Code != "minute_execution_disabled" {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
}
