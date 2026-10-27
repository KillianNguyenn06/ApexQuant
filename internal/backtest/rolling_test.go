package backtest

import (
	"apexquant/internal/marketdata"
	"testing"
)

func TestDailyRollingBandsCatchUpAndDetectRecoveredDipWithoutLookahead(t *testing.T) {
	c := dailyTouchConfig()
	c.DailyVWAPWindow = 20
	var bars []marketdata.BarTick
	for i := 0; i < 42; i++ {
		price := 100.0
		if i >= 20 {
			price = 200 + float64((i-20)%2)*2
		}
		if i >= 40 {
			price = 200.5
		}
		b := testBar("AAPL", i, price, price, price)
		if i == 40 {
			b.Low = 200
		}
		bars = append(bars, b)
	}
	c.BarsBySymbol["AAPL"] = bars
	r, err := RunBacktest(c)
	if err != nil {
		t.Fatal(err)
	}
	previous := r.Snapshots[39]
	touch := r.Snapshots[40]
	if previous.Indicator.VWAP != 201 || previous.Indicator.LowerBand != 200 {
		t.Fatalf("old regime still in window: %+v", previous.Indicator)
	}
	if touch.Bar.Close <= touch.Indicator.LowerBand || touch.Signal.Action != "Buy" || touch.SubmittedOrder == nil {
		t.Fatalf("recovered dip not submitted: %+v", touch)
	}
	if touch.FilledOrder != nil || r.Snapshots[41].FilledOrders[0].Action != "Buy" {
		t.Fatal("entry did not wait for next open")
	}
	// Changing a later candle cannot alter any earlier indicator or decision.
	bars[41].Open = 1000
	bars[41].Close = 1000
	bars[41].High = 1000
	bars[41].Low = 1000
	bars[41].VWAP = 1000
	changed, err := RunBacktest(c)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Snapshots[40].Indicator != touch.Indicator || changed.Snapshots[40].DecisionReason != touch.DecisionReason {
		t.Fatal("future candle changed earlier decision")
	}
}

func TestDailyVWAPWindowValidationAndDefault(t *testing.T) {
	c := dailyTouchConfig()
	if DailyVWAPWindowSize(c) != 20 {
		t.Fatal("wrong default")
	}
	for _, window := range []int{-1, 1, 253} {
		c.DailyVWAPWindow = window
		if _, err := RunBacktest(c); err == nil {
			t.Fatalf("accepted %d", window)
		}
	}
	c = minuteConfig()
	c.DailyVWAPWindow = 20
	if _, err := RunBacktest(c); err == nil {
		t.Fatal("daily window silently applied to minute execution")
	}
}
