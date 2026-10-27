package api

import (
	"apexquant/internal/backtest"
	"context"
	"testing"
	"time"
)

func TestDailyVWAPRequestValidation(t *testing.T) {
	r := validRequest()
	r.Timeframe = "1Day"
	r.VWAPWindow = 60
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	v, err := ValidateCreateBacktestRequest(r, now)
	if err != nil || v.Request.VWAPWindow != 60 {
		t.Fatalf("window lost: %+v %v", v, err)
	}
	for _, n := range []int{-1, 1, 253} {
		r.VWAPWindow = n
		if _, err := ValidateCreateBacktestRequest(r, now); err == nil {
			t.Fatalf("accepted %d", n)
		}
	}
	r.VWAPWindow = 20
	r.Timeframe = "1Min"
	if _, err := ValidateCreateBacktestRequest(r, now); err == nil {
		t.Fatal("accepted daily window in minute mode")
	}
}

func TestServiceRunnerRecordsEffectiveDailyVWAPWindow(t *testing.T) {
	service, err := backtest.NewService(httpProvider{}, backtest.Credentials{}, backtest.DefaultServiceSettings())
	if err != nil {
		t.Fatal(err)
	}
	request := validRequest()
	request.VWAPWindow = 60
	validated, err := ValidateCreateBacktestRequest(request, testNow())
	if err != nil {
		t.Fatal(err)
	}
	output, err := ServiceRunner(service, DataInfo{Timeframe: "1Day"})(context.Background(), "daily-window", validated)
	if err != nil {
		t.Fatal(err)
	}
	if output.Settings.DailyVWAPWindow != 60 || output.Request.VWAPWindow != 60 {
		t.Fatalf("window lost: %+v", output.Settings)
	}
}
