package backtest

import (
	"apexquant/internal/account"
	"apexquant/internal/marketdata"
	"apexquant/internal/session"
	"apexquant/internal/simulation"
	"math"
	"testing"
	"time"
)

func minuteConfig() BacktestConfig {
	start := time.Date(2026, 10, 5, 13, 30, 0, 0, time.UTC)
	var bars []marketdata.BarTick
	for i, p := range []float64{100, 102, 98, 99, 100} {
		bars = append(bars, marketdata.BarTick{Symbol: "AAPL", Timestamp: start.Add(time.Duration(i) * time.Minute), Open: p, High: p, Low: p, Close: p, VWAP: p, Volume: 100})
	}
	return BacktestConfig{Timeframe: "1Min", InitialAccount: account.Account{Cash: 50000, Equity: 50000, BuyingPower: 50000}, Allocations: []session.PortfolioAllocation{{Symbol: "AAPL", Weight: 1}}, BarsBySymbol: map[string][]marketdata.BarTick{"AAPL": bars}, VolatilityWindow: 20, PeriodsPerYear: 252, MonteCarloInput: simulation.MonteCarlo{Volatility: 0, RiskFreeRate: 2, TimeHorizon: 5.0 / 252, NumPaths: 10, NumSteps: 252, Seed: 42}}
}

func TestIntradayEntryExitAccountingAndNoReentry(t *testing.T) {
	c := minuteConfig()
	c.BarsBySymbol["AAPL"][3].Low = 90
	c.BarsBySymbol["AAPL"][3].High = 110
	r, err := RunBacktest(c)
	if err != nil {
		t.Fatal(err)
	}
	submitted := r.Snapshots[2]
	filled := r.Snapshots[3]
	if submitted.SubmittedOrder == nil || len(submitted.FilledOrders) != 0 {
		t.Fatalf("entry timing: %+v", submitted)
	}
	if !submitted.SubmittedOrder.CreatedAt.Equal(filled.Timestamp) {
		t.Fatal("signal must be known at the next minute open")
	}
	if len(filled.FilledOrders) != 2 || filled.ExecutionReason != "both_boundaries_stop_first" || filled.SubmittedOrder != nil || filled.Position.Quantity != 0 {
		t.Fatalf("OCO/reentry: %+v", filled)
	}
	buy, sell := filled.FilledOrders[0], filled.FilledOrders[1]
	want := 50000 + buy.Quantity*(sell.FilledPrice-buy.FilledPrice)
	if math.Abs(r.FinalAccount.Cash-want) > 1e-7 || r.FinalAccount.Cash != r.FinalAccount.Equity || r.FinalAccount.Cash != r.FinalAccount.BuyingPower {
		t.Fatalf("account: %+v, want %v", r.FinalAccount, want)
	}
	if submitted.Indicator.EMASlope >= 0 || submitted.DecisionReason != "buy_submitted" {
		t.Fatal("falling EMA must not veto dip buy")
	}
}

func TestProtectiveExitBoundaryAndGapPolicies(t *testing.T) {
	p := account.Position{StopLossPrice: 95, TakeProfitPrice: 105}
	for _, tc := range []struct {
		name                  string
		open, low, high, want float64
		reason                string
	}{
		{"stop equality", 100, 95, 104, 95, "stop_touch"}, {"target equality", 100, 96, 105, 105, "target_touch"},
		{"both", 100, 94, 106, 95, "both_boundaries_stop_first"}, {"gap down", 90, 89, 106, 90, "stop_gap_open"},
		{"gap up", 110, 90, 111, 110, "target_gap_open"}, {"hold", 100, 96, 104, 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			price, reason := protectiveExit(p, marketdata.BarTick{Open: tc.open, Low: tc.low, High: tc.high})
			if price != tc.want || reason != tc.reason {
				t.Fatalf("got %v %s", price, reason)
			}
		})
	}
}

func TestIntradayTouchRecoveredPriceAndKellyRejection(t *testing.T) {
	c := minuteConfig()
	b := &c.BarsBySymbol["AAPL"][2]
	b.Low = 100
	b.Open = 100.5
	b.Close = 100.5
	b.High = 100.5
	b.VWAP = 100.5
	// Previous band is exactly 100; close is above the current lower band.
	r, err := RunBacktest(c)
	if err != nil {
		t.Fatal(err)
	}
	if r.Snapshots[2].Signal.Action != "Buy" || r.Snapshots[2].SubmittedOrder == nil {
		t.Fatalf("touch lost: %+v", r.Snapshots[2])
	}
	c.MonteCarloInput.RiskFreeRate = -1
	r, err = RunBacktest(c)
	if err != nil {
		t.Fatal(err)
	}
	s := r.Snapshots[2]
	if s.DecisionReason != "buy_rejected_kelly" || s.Risk == nil || s.SubmittedOrder != nil || s.Risk.LossProbability != 1 {
		t.Fatalf("rejection unexplained: %+v", s)
	}
}

func TestIntradayMissingMinuteCancelsEntryAndSessionReset(t *testing.T) {
	c := minuteConfig()
	for i := 3; i < 5; i++ {
		c.BarsBySymbol["AAPL"][i].Timestamp = c.BarsBySymbol["AAPL"][i].Timestamp.Add(24 * time.Hour)
	}
	r, err := RunBacktest(c)
	if err != nil {
		t.Fatal(err)
	}
	s := r.Snapshots[3]
	if len(s.OrderEvents) != 1 || s.OrderEvents[0].Status != "canceled" || len(s.FilledOrders) != 0 || s.Indicator.VWAP != s.Bar.VWAP || s.Indicator.StandardDeviation != 0 {
		t.Fatalf("session: %+v", s)
	}
	c = minuteConfig()
	c.Allocations = []session.PortfolioAllocation{{Symbol: "AAPL", Weight: .5}, {Symbol: "BAC", Weight: .5}}
	c.BarsBySymbol["BAC"] = append([]marketdata.BarTick{}, c.BarsBySymbol["AAPL"]...)
	for i := range c.BarsBySymbol["BAC"] {
		c.BarsBySymbol["BAC"][i].Symbol = "BAC"
	}
	c.BarsBySymbol["BAC"][3].Timestamp = c.BarsBySymbol["BAC"][3].Timestamp.Add(30 * time.Second)
	if _, err := RunBacktest(c); err == nil {
		t.Fatal("unaligned data accepted")
	}
}

func TestIntradayDailyVolatilityExcludesCurrentAndFutureSession(t *testing.T) {
	c := minuteConfig()
	loc, _ := time.LoadLocation("America/New_York")
	var history []marketdata.BarTick
	for i, p := range []float64{90, 100, 95, 9999} {
		history = append(history, marketdata.BarTick{Timestamp: time.Date(2026, 10, 2+i, 4, 0, 0, 0, time.UTC), Close: p})
	}
	c.DailyVolatilityBars = map[string][]marketdata.BarTick{"AAPL": history}
	got, err := intradayVolatility(c, "AAPL", "2026-10-05", loc)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := simulation.AnnualizedVolatility(history[:3], 252)
	if got != want {
		t.Fatalf("lookahead: got %v want %v", got, want)
	}
}

func TestIntradayLastBarAndZeroVolumeAreExplicit(t *testing.T) {
	c := minuteConfig()
	c.BarsBySymbol["AAPL"] = c.BarsBySymbol["AAPL"][:3]
	r, err := RunBacktest(c)
	if err != nil {
		t.Fatal(err)
	}
	if s := r.Snapshots[2]; s.Signal.Action != "Buy" || s.SubmittedOrder != nil || s.DecisionReason != "buy_not_submitted_final_bar" {
		t.Fatalf("last bar: %+v", s)
	}
	c = minuteConfig()
	c.BarsBySymbol["AAPL"][3].Volume = 0
	r, err = RunBacktest(c)
	if err != nil {
		t.Fatal(err)
	}
	if s := r.Snapshots[3]; len(s.FilledOrders) != 0 || len(s.OrderEvents) != 1 || s.DecisionReason != "no_volume" {
		t.Fatalf("zero liquidity: %+v", s)
	}
}

func TestIntradayAllocationCapsAndPortfolioReconciliation(t *testing.T) {
	c := minuteConfig()
	c.Allocations = []session.PortfolioAllocation{{Symbol: "AAPL", Weight: .5}, {Symbol: "BAC", Weight: .5}}
	c.BarsBySymbol["BAC"] = append([]marketdata.BarTick{}, c.BarsBySymbol["AAPL"]...)
	for i := range c.BarsBySymbol["BAC"] {
		c.BarsBySymbol["BAC"][i].Symbol = "BAC"
	}
	r, err := RunBacktest(c)
	if err != nil {
		t.Fatal(err)
	}
	cash := 50000.0
	positions := map[string]float64{}
	for i := 0; i < len(r.Snapshots); i += 2 {
		equity := cash
		for _, s := range r.Snapshots[i : i+2] {
			for _, o := range s.FilledOrders {
				value := o.Quantity * o.FilledPrice
				if o.Action == "Buy" {
					cash -= value
					positions[o.Symbol] += o.Quantity
					if value > 25000.0000001 {
						t.Fatal("allocation cap exceeded")
					}
				} else {
					cash += value
					positions[o.Symbol] -= o.Quantity
				}
			}
		}
		equity = cash
		for _, s := range r.Snapshots[i : i+2] {
			equity += positions[s.Bar.Symbol] * s.Bar.Close
			if math.Abs(s.Position.Quantity-positions[s.Bar.Symbol]) > 1e-8 {
				t.Fatal("position mismatch")
			}
		}
		for _, s := range r.Snapshots[i : i+2] {
			if math.Abs(s.Account.Equity-equity) > 1e-7 || math.Abs(s.Account.Cash-cash) > 1e-7 || cash < -1e-7 {
				t.Fatalf("portfolio mismatch: %+v", s.Account)
			}
		}
	}
	// A held position must not queue another dip entry.
	if r.Snapshots[6].SubmittedOrder != nil || r.Snapshots[7].SubmittedOrder != nil {
		t.Fatal("duplicate entry")
	}
}
