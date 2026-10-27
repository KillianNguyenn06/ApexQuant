package api

import (
	"apexquant/internal/backtest"
	"context"
	"strings"
	"testing"
	"time"
)

func TestModelSettingsValidationAndPropagation(t *testing.T) {
	r := CreateBacktestRequest{InitialCapital: 10000, Allocations: []Allocation{{Symbol: "AAPL", Percent: 100}}, StartDate: "2025-01-01", EndDate: "2025-01-04", HoldingDays: 3, ProbabilityModel: "vwap_reversion", ReversionHalfLifeDays: 8}
	validated, e := ValidateCreateBacktestRequest(r, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	settings := backtest.DefaultServiceSettings()
	settings.MonteCarlo.NumPaths = 10
	settings.MonteCarlo.Seed = 42
	service, e := backtest.NewService(tradingProvider{}, backtest.Credentials{}, settings)
	if e != nil {
		t.Fatal(e)
	}
	// tradingProvider fixture dates are fixed; use its existing HTTP request dates.
	validated.Start = time.Date(2025, 9, 5, 4, 0, 0, 0, time.UTC)
	validated.End = time.Date(2025, 9, 9, 4, 0, 0, 0, time.UTC)
	result, e := ServiceRunner(service, DataInfo{})(context.Background(), "test", validated)
	if e != nil {
		t.Fatal(e)
	}
	if result.Settings.HoldingDays != 3 || result.Settings.TimeHorizon != 3.0/252 || result.Settings.ProbabilityModel != "vwap_reversion" || result.Settings.ReversionHalfLifeDays != 8 {
		t.Fatalf("settings not propagated: %+v", result.Settings)
	}
	for _, change := range []func(*CreateBacktestRequest){func(r *CreateBacktestRequest) { r.HoldingDays = -1 }, func(r *CreateBacktestRequest) { r.HoldingDays = 253 }, func(r *CreateBacktestRequest) { r.ProbabilityModel = "unknown" }, func(r *CreateBacktestRequest) { r.ReversionHalfLifeDays = -1 }, func(r *CreateBacktestRequest) { r.ProbabilityModel = "gbm" }} {
		invalid := r
		change(&invalid)
		if _, e := ValidateCreateBacktestRequest(invalid, time.Now()); e == nil {
			t.Fatal("invalid model settings accepted")
		}
	}
}

func TestAutoReversionRejectsUnreliableWithoutFallback(t *testing.T) {
	r := CreateBacktestRequest{InitialCapital: 10000, Allocations: []Allocation{{Symbol: "AAPL", Percent: 100}}, StartDate: "2025-09-05", EndDate: "2025-09-09", ProbabilityModel: "vwap_reversion_auto"}
	v, e := ValidateCreateBacktestRequest(r, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	service, e := backtest.NewService(tradingProvider{}, backtest.Credentials{}, backtest.DefaultServiceSettings())
	if e != nil {
		t.Fatal(e)
	}
	jobs, e := NewJobManager(ServiceRunner(service, DataInfo{}), DefaultJobOptions())
	if e != nil {
		t.Fatal(e)
	}
	defer jobs.Close(context.Background())
	_, failure := jobs.execute(&job{id: "auto", request: v})
	if failure == nil || failure.Code != "unreliable_reversion" || !strings.Contains(failure.Message, "Choose Manual or Off") {
		t.Fatalf("missing actionable error %+v", failure)
	}
	r.ReversionHalfLifeDays = 5
	if _, e := ValidateCreateBacktestRequest(r, time.Now()); e == nil {
		t.Fatal("auto accepted hidden manual override")
	}
}
