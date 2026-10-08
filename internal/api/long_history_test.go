package api

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"apexquant/internal/account"
	"apexquant/internal/backtest"
	"apexquant/internal/marketdata"
	"apexquant/internal/session"
	"apexquant/internal/simulation"
)

func TestDashboardAdvertisesExpandedDefaults(t *testing.T) {
	h, _ := httpTestHandler(t, func(context.Context, string, ValidatedRequest) (BacktestResultResponse, error) {
		t.Fatal("loading limits must not calculate a backtest")
		return BacktestResultResponse{}, nil
	})
	w := requestHTTP(h, "GET", "/api/config", "", "")
	var config DashboardConfig
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &config) != nil || config.MaxRangeDays != 3660 || config.MaxSnapshots != 29280 {
		t.Fatalf("dashboard did not receive expanded limits: %s", w.Body.String())
	}
}

func TestLongHistoryAdmissionAndBoundaries(t *testing.T) {
	opts := DefaultHandlerOptions()
	r := validRequest()
	r.StartDate = "2021-01-01"
	r.EndDate = "2026-10-06"
	validated, err := ValidateCreateBacktestRequest(r, time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateRequestLimits(validated, opts.MaxRangeDays, opts.MaxSnapshots); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRequestLimits(validated, 366, 8*366); err == nil {
		t.Fatal("smaller server override was ignored")
	}
	start := time.Date(2017, 1, 1, 0, 0, 0, 0, time.UTC)
	r.StartDate = start.Format(time.DateOnly)
	r.EndDate = start.AddDate(0, 0, opts.MaxRangeDays-1).Format(time.DateOnly)
	r.Allocations = nil
	for i := 0; i < MaxSymbols; i++ {
		r.Allocations = append(r.Allocations, Allocation{Symbol: fmt.Sprintf("S%d", i), Percent: 100.0 / MaxSymbols})
	}
	validated, err = ValidateCreateBacktestRequest(r, time.Date(2028, 1, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateRequestLimits(validated, opts.MaxRangeDays, opts.MaxSnapshots); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRequestLimits(validated, opts.MaxRangeDays, opts.MaxSnapshots-1); err == nil {
		t.Fatal("snapshot ceiling was not enforced")
	}
	validated.Request.EndDate = start.AddDate(0, 0, opts.MaxRangeDays).Format(time.DateOnly)
	if err := ValidateRequestLimits(validated, opts.MaxRangeDays, opts.MaxSnapshots); err == nil {
		t.Fatal("accepted one day beyond the limit")
	}
}

func TestMaximumLongHistoryEngineResultAndReplayFitDefaultBudgets(t *testing.T) {
	opts := DefaultHandlerOptions()
	jobs := DefaultJobOptions()
	start := time.Date(2017, 1, 1, 12, 0, 0, 0, time.UTC)
	request := CreateBacktestRequest{InitialCapital: 50000, StartDate: start.Format(time.DateOnly), EndDate: start.AddDate(0, 0, opts.MaxRangeDays-1).Format(time.DateOnly)}
	cfg := backtest.BacktestConfig{InitialAccount: account.Account{Cash: 50000, BuyingPower: 50000, Equity: 50000}, BarsBySymbol: map[string][]marketdata.BarTick{}, VolatilityWindow: 20, PeriodsPerYear: 252, MonteCarloInput: simulation.MonteCarlo{Volatility: .2, TimeHorizon: 1, NumPaths: 1, NumSteps: 1, Seed: 42}}
	for stock := 0; stock < MaxSymbols; stock++ {
		symbol := fmt.Sprintf("S%d", stock)
		price := 100.0 + float64(stock)
		cfg.Allocations = append(cfg.Allocations, session.PortfolioAllocation{Symbol: symbol, Weight: 1.0 / MaxSymbols})
		request.Allocations = append(request.Allocations, Allocation{Symbol: symbol, Percent: 100.0 / MaxSymbols})
		bars := make([]marketdata.BarTick, opts.MaxRangeDays)
		for day := range bars {
			bars[day] = marketdata.BarTick{Symbol: symbol, Timestamp: start.AddDate(0, 0, day), Open: price, High: price, Low: price, Close: price, VWAP: price, Volume: 1000}
		}
		cfg.BarsBySymbol[symbol] = bars
	}
	// Flat prices produce Hold without Monte Carlo evaluation. This exercises the
	// maximum history/mapping/serialization workload, not simulation throughput.
	result, err := backtest.RunBacktest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Snapshots) != opts.MaxSnapshots || result.FinalAccount.Equity != 50000 {
		t.Fatal("long engine history was truncated or changed accounting")
	}
	mapped, err := NewResultResponse("long-history", request, cfg, DataInfo{Feed: "synthetic", Timeframe: "1Day", Adjustment: "raw"}, result)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(mapped)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(encoded)) > jobs.MaxResultBytes || jobs.MaxStoredBytes < jobs.MaxResultBytes {
		t.Fatal("default result budgets cannot retain maximum history")
	}
	timeline, err := newReplayTimeline(mapped, opts.Replay.MaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	want := 2 + opts.MaxRangeDays*(MaxSymbols+1)
	if len(timeline.events) != want || timeline.days != opts.MaxRangeDays {
		t.Fatal("long replay skipped events")
	}
	first := decodeReplay[ReplaySnapshotData](t, timeline.events[1])
	last := decodeReplay[ReplayCompleteData](t, timeline.events[want-1])
	if first.Snapshot.Timestamp.Format(time.DateOnly) != request.StartDate || last.TotalSnapshots != opts.MaxSnapshots || last.FinalAccount != mapped.FinalAccount {
		t.Fatal("long replay endpoints disagree with engine result")
	}
	t.Logf("days=%d snapshots=%d events=%d result_bytes=%d", opts.MaxRangeDays, opts.MaxSnapshots, want, len(encoded))
}
