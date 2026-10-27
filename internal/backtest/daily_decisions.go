package backtest

import (
	"apexquant/internal/account"
	"apexquant/internal/algorithm"
	"apexquant/internal/broker"
	"apexquant/internal/marketdata"
	"apexquant/internal/risk"
	"apexquant/internal/session"
	"apexquant/internal/simulation"
)

// Explain each daily decision without treating a rejected opportunity as a fill.
// Candidate entry levels must not leak into a flat position after rejection.
func dailyDecision(indicator algorithm.Indicator, previous algorithm.Indicator, position account.Position, bar marketdata.BarTick, input simulation.MonteCarlo, acc account.Account, weight float64, last bool) (algorithm.Signal, account.Position, *account.Order, *session.RiskDecision, string) {
	candidate := position
	signal := algorithm.Signal{Symbol: bar.Symbol, Action: "Hold", Price: bar.Close, VWAP: indicator.VWAP, CreatedAt: bar.Timestamp}
	if position.Quantity > 0 {
		return signal, position, nil, nil, "position_open"
	}
	if bar.Volume <= 0 {
		return signal, position, nil, nil, "no_volume"
	}
	if indicator.StandardDeviation <= 0 {
		return signal, position, nil, nil, "bands_not_ready"
	}
	touched := previous.StandardDeviation > 0 && bar.Low <= previous.LowerBand
	if !touched && bar.Close > indicator.LowerBand {
		reason := "inside_bands"
		if bar.Close > indicator.UpperBand {
			reason = "above_upper_band_no_breakout_entry"
		}
		return signal, position, nil, nil, reason
	}
	signal.Action = "Buy"
	candidate.EntryPrice = bar.Close
	candidate.StopLossPrice = bar.Close - indicator.StandardDeviation
	candidate.TakeProfitPrice = indicator.VWAP + .5*indicator.StandardDeviation

	if last {
		return signal, position, nil, nil, "order_not_submitted_final_bar"
	}
	if candidate.StopLossPrice <= 0 || candidate.TakeProfitPrice <= candidate.EntryPrice {
		return signal, position, nil, nil, "buy_rejected_invalid_reward_or_stop"
	}
	if acc.Cash <= 0 || acc.BuyingPower <= 0 {
		return signal, position, nil, nil, "buy_rejected_funds"
	}
	if acc.Equity*weight <= position.Quantity*position.CurrentPrice {
		return signal, position, nil, nil, "buy_rejected_allocation"
	}
	model := simulation.MonteCarloResult{}
	simulation.GBMModel(input, candidate, &model, signal)
	fraction := risk.KellyCriterion(model, candidate)
	qty := risk.EvaluateRisk(model, candidate, acc, weight)
	diagnostic := &session.RiskDecision{RiskFreeRate: input.RiskFreeRate, Model: input.Model, TimeoutMeanReturn: model.TimeoutMeanReturn, WinProbability: model.WinProbability, LossProbability: model.LossProbability, TimeoutProbability: model.TimeoutProbability, KellyFraction: fraction, Quantity: qty}
	if fraction <= 0 {
		return signal, position, nil, diagnostic, "buy_rejected_kelly"
	}
	if qty <= 0 {
		return signal, position, nil, diagnostic, "buy_rejected_sizing"
	}
	order := broker.SubmitOrder(signal, qty)
	return signal, candidate, &order, diagnostic, "buy_submitted"
}
