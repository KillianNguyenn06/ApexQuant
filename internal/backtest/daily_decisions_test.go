package backtest

import (
	"apexquant/internal/account"
	"apexquant/internal/algorithm"
	"apexquant/internal/marketdata"
	"apexquant/internal/simulation"
	"testing"
)

func TestDailyDecisionAudit(t *testing.T) {
	indicator := algorithm.Indicator{VWAP: 100, StandardDeviation: 2, LowerBand: 98, UpperBand: 102}
	flat := account.Position{Symbol: "AAPL", CurrentPrice: 95}
	open := account.Position{Symbol: "AAPL", Quantity: 1, EntryPrice: 100, CurrentPrice: 100, StopLossPrice: 95, TakeProfitPrice: 105}
	capital := account.Account{Cash: 10000, BuyingPower: 10000, Equity: 10000}
	input := simulation.MonteCarlo{Volatility: 0, RiskFreeRate: 1, TimeHorizon: 1, NumPaths: 10, NumSteps: 10, Seed: 42}
	tests := []struct {
		name             string
		p                account.Position
		close, low, high float64
		last             bool
		reason, action   string
	}{
		{"flat inside", flat, 100, 99, 101, false, "inside_bands", "Hold"},
		{"surge has no breakout entry", flat, 110, 99, 111, false, "above_upper_band_no_breakout_entry", "Hold"},
		{"touch and recover", flat, 100, 98, 101, false, "buy_submitted", "Buy"},
		{"exact boundary close", flat, 98, 98, 100, false, "buy_submitted", "Buy"},
		{"final opportunity", flat, 95, 94, 96, true, "order_not_submitted_final_bar", "Buy"},
		{"holding inside", open, 100, 99, 101, false, "position_open", "Hold"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.p
			p.CurrentPrice = tc.close
			b := marketdata.BarTick{Symbol: "AAPL", Open: tc.close, Close: tc.close, Low: tc.low, High: tc.high, Volume: 100}
			signal, candidate, order, diagnostic, reason := dailyDecision(indicator, indicator, p, b, input, capital, 1, tc.last)
			if signal.Action != tc.action || reason != tc.reason {
				t.Fatalf("got %s / %s", signal.Action, reason)
			}
			if tc.reason == "buy_submitted" {
				if order == nil || diagnostic == nil || diagnostic.Quantity <= 0 || candidate.EntryPrice != tc.close {
					t.Fatal("missing entry evidence")
				}
			}
			if tc.last && order != nil {
				t.Fatal("submitted without next bar")
			}
			if order == nil && candidate != p {
				t.Fatal("rejected decision mutated position")
			}
		})
	}
}

func TestDailyRejectedOpportunityHasProbabilitiesAndDoesNotChangePosition(t *testing.T) {
	indicator := algorithm.Indicator{VWAP: 100, StandardDeviation: 2, LowerBand: 98, UpperBand: 102}
	p := account.Position{Symbol: "AAPL", CurrentPrice: 95}
	b := marketdata.BarTick{Symbol: "AAPL", Open: 95, High: 96, Low: 94, Close: 95, Volume: 100}
	acc := account.Account{Equity: 1000, Cash: 1000, BuyingPower: 1000}
	input := simulation.MonteCarlo{TimeHorizon: 1, NumSteps: 10, NumPaths: 10, Seed: 42}
	signal, after, order, diagnostic, reason := dailyDecision(indicator, indicator, p, b, input, acc, 1, false)
	if signal.Action != "Buy" || order != nil || after != p || reason != "buy_rejected_kelly" || diagnostic == nil || diagnostic.TimeoutProbability != 1 || diagnostic.Quantity != 0 {
		t.Fatalf("unexplained rejection: %s %+v", reason, diagnostic)
	}
	acc.Cash = 0
	_, _, _, diagnostic, reason = dailyDecision(indicator, indicator, p, b, input, acc, 1, false)
	if reason != "buy_rejected_funds" || diagnostic != nil {
		t.Fatal("funds rejection misclassified as Kelly")
	}
}

func TestDailyEngineExplainsEveryBarAndPreservesAuditOnRejectedEntry(t *testing.T) {
	c := boundaryConfig()
	c.BarsBySymbol["AAPL"][1] = testBar("AAPL", 1, 100, 90, 110)
	c.MonteCarloInput.RiskFreeRate = -1000
	result, err := RunBacktest(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range result.Snapshots {
		if s.DecisionReason == "" {
			t.Fatal("missing daily reason")
		}
	}
	s := result.Snapshots[1]
	if s.Signal.Action != "Buy" || s.DecisionReason != "buy_rejected_kelly" || s.Risk == nil || s.SubmittedOrder != nil || s.Position.EntryPrice != 0 {
		t.Fatalf("missing rejection evidence: %+v", s)
	}
	reconcile(t, c, result)
}

func dailyTouchConfig() BacktestConfig {
	c := minuteConfig()
	c.Timeframe = "1Day"
	for i := range c.BarsBySymbol["AAPL"] {
		c.BarsBySymbol["AAPL"][i].Timestamp = testBar("AAPL", i, 100, 100, 100).Timestamp
	}
	// Known previous lower band is 100. Today's recovered close is above its
	// current lower band, so this entry depends specifically on the prior touch.
	b := &c.BarsBySymbol["AAPL"][2]
	b.Open = 100.5
	b.High = 100.5
	b.Low = 100
	b.Close = 100.5
	b.VWAP = 100.5
	c.BarsBySymbol["AAPL"][3] = testBar("AAPL", 3, 100.5, 100.5, 100.5)
	c.BarsBySymbol["AAPL"][4] = testBar("AAPL", 4, 100.5, 100.5, 100.5)
	c.MonteCarloInput.RiskFreeRate = 1000 // Deterministically favorable fixture; not a market assumption.
	return c
}

func TestDailyPriorBandTouchEntryAndProtectiveExits(t *testing.T) {
	for _, tc := range []struct {
		name      string
		low, high float64
		reason    string
	}{
		{"stop wick recovered close", 95, 100.5, "stop_touch"},
		{"target wick recovered close", 100.5, 105, "target_touch"},
		{"both touched", 95, 105, "both_boundaries_stop_first"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := dailyTouchConfig()
			// The protective exit is on the final bar: no later open is needed.
			c.BarsBySymbol["AAPL"][4].Low = tc.low
			c.BarsBySymbol["AAPL"][4].High = tc.high
			r, err := RunBacktest(c)
			if err != nil {
				t.Fatal(err)
			}
			opportunity := r.Snapshots[2]
			if opportunity.Bar.Close <= opportunity.Indicator.LowerBand || opportunity.Signal.Action != "Buy" || opportunity.SubmittedOrder == nil {
				t.Fatalf("recovered touch lost: %+v", opportunity)
			}
			entry := r.Snapshots[3]
			last := r.Snapshots[4]
			if len(entry.FilledOrders) != 1 || entry.FilledOrders[0].Action != "Buy" || entry.FilledOrders[0].FilledPrice != 100.5 {
				t.Fatal("entry must fill next open")
			}
			if len(last.FilledOrders) != 1 || last.FilledOrders[0].Action != "Sell" || last.ExecutionReason != tc.reason || last.Position.Quantity != 0 || last.SubmittedOrder != nil {
				t.Fatalf("exit lost: %+v", last)
			}
			expected := entry.Position.StopLossPrice
			if tc.reason == "target_touch" {
				expected = entry.Position.TakeProfitPrice
			}
			if last.FilledOrders[0].FilledPrice != expected {
				t.Fatalf("exit %v want %v", last.FilledOrders[0].FilledPrice, expected)
			}
			reconcile(t, c, r)
		})
	}
}

func TestDailyEntryCancellationIsRetained(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		change       func(*marketdata.BarTick)
	}{
		{"no volume", "entry_canceled_no_volume", func(b *marketdata.BarTick) { b.Volume = 0 }},
		{"invalid gap stop", "entry_rejected_invalid_gap_stop", func(b *marketdata.BarTick) { b.Open = 0.1; b.Low = 0.1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := dailyTouchConfig()
			c.BarsBySymbol["AAPL"] = c.BarsBySymbol["AAPL"][:4]
			tc.change(&c.BarsBySymbol["AAPL"][3])
			r, err := RunBacktest(c)
			if err != nil {
				t.Fatal(err)
			}
			s := r.Snapshots[3]
			if len(s.OrderEvents) != 1 || len(s.FilledOrders) != 0 || s.ExecutionReason != tc.reason || s.Position.EntryPrice != 0 || s.Position.Quantity != 0 {
				t.Fatalf("silent cancellation: %+v", s)
			}
		})
	}
}
