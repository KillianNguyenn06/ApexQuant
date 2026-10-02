package api

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"apexquant/internal/account"
	"apexquant/internal/algorithm"
	"apexquant/internal/backtest"
	"apexquant/internal/marketdata"
	"apexquant/internal/session"
)

func TestEnvelopeJSON(t *testing.T) {
	cases := []struct {
		value any
		want  string
	}{
		{CreateBacktestResponse{ID: "job-1", Status: StatusQueued}, `{"id":"job-1","status":"queued"}`},
		{JobStatusResponse{ID: "job-1", Status: StatusRunning}, `{"id":"job-1","status":"running","error":null}`},
		{ErrorResponse{Error: &APIError{Code: "invalid_allocations", Message: "Allocation percentages must total 100.", Field: "allocations"}}, `{"error":{"code":"invalid_allocations","message":"Allocation percentages must total 100.","field":"allocations"}}`},
		{JobStatusResponse{ID: "job-1", Status: StatusFailed, Error: &APIError{Code: "provider_error", Message: "Market data could not be loaded."}}, `{"id":"job-1","status":"failed","error":{"code":"provider_error","message":"Market data could not be loaded."}}`},
	}
	for _, tc := range cases {
		got, err := json.Marshal(tc.value)
		if err != nil || string(got) != tc.want {
			t.Fatalf("got %s err=%v want %s", got, err, tc.want)
		}
	}
}
func TestSnapshotMapping(t *testing.T) {
	makeSnapshot := func(symbol string, volume float64) session.BacktestSnapshot {
		return session.BacktestSnapshot{Timestamp: testNow(), Bar: marketdata.BarTick{Symbol: symbol, Open: 100, High: 100, Low: 100, Close: 100, Volume: volume, VWAP: 100, Timestamp: testNow()}, Indicator: algorithm.Indicator{VWAP: 100, UpperBand: 100, LowerBand: 100}, Signal: algorithm.Signal{Action: "Hold"}, Position: account.Position{Symbol: symbol, CurrentPrice: 100, EntryPrice: 99}, Account: account.Account{Cash: 50000, BuyingPower: 50000, Equity: 50000}}
	}
	result := backtest.BacktestResult{FinalAccount: account.Account{Cash: 50000, Equity: 50000, BuyingPower: 50000}, FinalPositions: map[string]account.Position{"AAPL": {Symbol: "AAPL", CurrentPrice: 100, EntryPrice: 99}}, Snapshots: []session.BacktestSnapshot{makeSnapshot("AAPL", 0), makeSnapshot("MSFT", 100), makeSnapshot("AAPL", 0), makeSnapshot("AAPL", 100), makeSnapshot("AAPL", 0)}}
	response, err := NewResultResponse("job-1", validRequest(), backtest.BacktestConfig{}, DataInfo{}, result)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range []int{0, 2} {
		if response.Snapshots[i].Indicator.VWAP != nil {
			t.Fatal("another symbol's volume enabled indicators")
		}
	}
	for _, i := range []int{1, 3, 4} {
		s := response.Snapshots[i]
		if s.Indicator.StandardDeviation == nil || *s.Indicator.StandardDeviation != 0 || *s.Indicator.VWAP != 100 {
			t.Fatal("valid zero or accumulated indicator lost")
		}
	}
	if response.Snapshots[4].Bar.VWAP != nil {
		t.Fatal("zero-volume bar VWAP presented as available")
	}
	if response.FinalPositions["AAPL"].EntryPrice != nil {
		t.Fatal("flat position exposes stale entry price")
	}
	hold := response.Snapshots[0].Signal
	if hold.Symbol != "AAPL" || hold.Action != "hold" || hold.CreatedAt.IsZero() || hold.Price != nil {
		t.Fatal("hold mapping incorrect")
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{`"Equity"`, `"win_probability"`, `"z_score"`} {
		if strings.Contains(string(encoded), unwanted) {
			t.Fatalf("leaked internal field %s", unwanted)
		}
	}
	if !strings.Contains(string(encoded), `"standard_deviation":null`) || !strings.Contains(string(encoded), `"standard_deviation":0`) || !strings.Contains(string(encoded), `"submitted_order":null`) {
		t.Fatal("null and zero serialization contract broken")
	}
	*response.Snapshots[1].Indicator.VWAP = 999
	if result.Snapshots[1].Indicator.VWAP != 100 {
		t.Fatal("mapping aliases engine values")
	}
}
func TestOrderAndPositionMapping(t *testing.T) {
	submitted := account.Order{ID: "order-1", Symbol: "AAPL", Action: "Buy", Status: "Submitted", Quantity: 10, CreatedAt: testNow()}
	if mapOrder(&submitted).FilledPrice != nil {
		t.Fatal("pending order has a fill price")
	}
	filled := submitted
	filled.Status = "filled"
	filled.FilledPrice = 101
	mapped := mapOrder(&filled)
	if mapped.Action != "buy" || *mapped.FilledPrice != 101 {
		t.Fatal("incorrect fill mapping")
	}
	*mapped.FilledPrice = 5
	if filled.FilledPrice != 101 {
		t.Fatal("order aliases input")
	}
	p := mapPosition(account.Position{Symbol: "AAPL", Quantity: 10, EntryPrice: 101, StopLossPrice: 99, TakeProfitPrice: 105})
	if *p.EntryPrice != 101 || *p.StopLossPrice != 99 || *p.TakeProfitPrice != 105 {
		t.Fatal("open position missing levels")
	}
}
func TestNonFiniteResultRejected(t *testing.T) {
	result := backtest.BacktestResult{FinalAccount: account.Account{Equity: math.NaN()}}
	if _, err := NewResultResponse("job-1", validRequest(), backtest.BacktestConfig{}, DataInfo{}, result); err == nil {
		t.Fatal("accepted NaN result")
	}
	result.FinalAccount.Equity = 50000
	if _, err := NewResultResponse("job-1", validRequest(), backtest.BacktestConfig{}, DataInfo{RiskFreeRate: math.Inf(1)}, result); err == nil {
		t.Fatal("accepted infinite metadata")
	}
}
func TestEngineResultContract(t *testing.T) {
	validated, err := ValidateCreateBacktestRequest(validRequest(), testNow())
	if err != nil {
		t.Fatal(err)
	}
	c := backtest.BacktestConfig{InitialAccount: account.Account{Cash: 50000, Equity: 50000, BuyingPower: 50000}, Allocations: validated.Allocations, BarsBySymbol: map[string][]marketdata.BarTick{}, VolatilityWindow: 20, PeriodsPerYear: 252}
	c.MonteCarloInput.NumPaths = 10
	c.MonteCarloInput.NumSteps = 5
	c.MonteCarloInput.TimeHorizon = 5.0 / 252
	c.MonteCarloInput.Seed = 42
	for _, a := range c.Allocations {
		for day := 0; day < 3; day++ {
			c.BarsBySymbol[a.Symbol] = append(c.BarsBySymbol[a.Symbol], marketdata.BarTick{Symbol: a.Symbol, Open: 100, High: 100, Low: 100, Close: 100, VWAP: 100, Volume: 100, Timestamp: validated.Start.AddDate(0, 0, day)})
		}
	}
	result, err := backtest.RunBacktest(c)
	if err != nil {
		t.Fatal(err)
	}
	response, err := NewResultResponse("job-1", validated.Request, c, DataInfo{Feed: "sip", Timeframe: "1Day", Adjustment: "raw"}, result)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Snapshots) != 6 || response.Settings.Seed != 42 || response.FinalAccount.Equity != 50000 || response.Request.Allocations[0].Symbol != "AAPL" {
		t.Fatal("engine-to-API contract mismatch")
	}
}
