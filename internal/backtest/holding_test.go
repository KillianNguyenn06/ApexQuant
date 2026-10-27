package backtest

import (
	"apexquant/internal/marketdata"
	"testing"
)

func TestHoldingDeadlineCountsFromFill(t *testing.T) {
	for _, holding := range []int{1, 2, 5} {
		c := dailyTouchConfig()
		c.HoldingDays = holding
		for i := 5; i < 10; i++ {
			c.BarsBySymbol["AAPL"] = append(c.BarsBySymbol["AAPL"], testBar("AAPL", i, 100.5, 100.5, 100.5))
		}
		result, err := RunBacktest(c)
		if err != nil {
			t.Fatal(err)
		}
		expiry := 3 + holding - 1
		s := result.Snapshots[expiry]
		if s.ExecutionReason != "holding_expiry_close" || s.Position.Quantity != 0 || s.SubmittedOrder != nil {
			t.Fatalf("holding %d expiry %+v", holding, s)
		}
		for i := 3; i < expiry; i++ {
			if result.Snapshots[i].Position.Quantity <= 0 {
				t.Fatalf("premature exit day %d", i)
			}
		}
		reconcile(t, c, result)
	}
}
func TestHoldingDeadlineProtectivePriorityAndZeroVolume(t *testing.T) {
	c := dailyTouchConfig()
	c.HoldingDays = 2
	c.BarsBySymbol["AAPL"][4].Low = 95
	r, e := RunBacktest(c)
	if e != nil {
		t.Fatal(e)
	}
	if r.Snapshots[4].ExecutionReason != "stop_touch" {
		t.Fatal("expiry overrode protective stop")
	}
	c = dailyTouchConfig()
	c.BarsBySymbol["AAPL"][4].Volume = 0
	c.BarsBySymbol["AAPL"][4].VWAP = 0
	// Deadline zero-volume handling is exercised on an established position.
	c.HoldingDays = 2
	c.BarsBySymbol["AAPL"] = append(c.BarsBySymbol["AAPL"], testBar("AAPL", 5, 100.5, 100.5, 100.5))
	c.BarsBySymbol["AAPL"][5].Low = 95 // later stop touch must not precede an overdue open exit
	r, e = RunBacktest(c)
	if e != nil {
		t.Fatal(e)
	}
	if r.Snapshots[4].ExecutionReason != "exit_unavailable_zero_volume" || r.Snapshots[5].ExecutionReason != "holding_expiry_delayed_open" {
		t.Fatal("unavailable deadline fill not deferred")
	}
	reconcile(t, c, r)
}
func TestHistoricalRatesDoNotLeakFutureIntoDecision(t *testing.T) {
	c := dailyTouchConfig()
	c.RiskFreeRates = []marketdata.RateObservation{{Date: "2000-01-01", AvailableDate: "2000-01-02", Rate: 1000}, {Date: "2099-01-01", AvailableDate: "2099-01-02", Rate: -1000}}
	a, e := RunBacktest(c)
	if e != nil {
		t.Fatal(e)
	}
	c.RiskFreeRates[1].Rate = 1000
	b, e := RunBacktest(c)
	if e != nil {
		t.Fatal(e)
	}
	if a.Snapshots[2].Risk == nil || *a.Snapshots[2].Risk != *b.Snapshots[2].Risk || a.Snapshots[2].Risk.RiskFreeRate != 1000 {
		t.Fatal("future rate changed earlier decision")
	}
}
