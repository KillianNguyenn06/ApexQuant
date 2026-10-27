package backtest

import (
	"fmt"
	"time"

	"apexquant/internal/account"
	"apexquant/internal/algorithm"
	"apexquant/internal/broker"
	"apexquant/internal/marketdata"
	"apexquant/internal/risk"
	"apexquant/internal/session"
	"apexquant/internal/simulation"
)

// runIntraday processes aligned, observed minutes. Missing symbol data is an
// error, never fabricated liquidity. Price gaps are handled at the next open.
func runIntraday(c BacktestConfig, allocations []session.PortfolioAllocation) (BacktestResult, error) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return BacktestResult{}, err
	}
	states := map[string]*session.SymbolState{}
	ref := c.BarsBySymbol[allocations[0].Symbol]
	for _, a := range allocations {
		bars := c.BarsBySymbol[a.Symbol]
		if len(bars) != len(ref) {
			return BacktestResult{}, fmt.Errorf("minute timeline mismatch for %s; missing bars must be resolved", a.Symbol)
		}
		for i, b := range bars {
			if !b.Timestamp.Equal(ref[i].Timestamp) {
				return BacktestResult{}, fmt.Errorf("minute timestamp mismatch for %s at %d", a.Symbol, i)
			}
			local := b.Timestamp.In(loc)
			m := local.Hour()*60 + local.Minute()
			if m < 570 || m >= 960 {
				return BacktestResult{}, fmt.Errorf("minute outside configured 09:30-16:00 window for %s", a.Symbol)
			}
		}
		if history := c.DailyVolatilityBars[a.Symbol]; len(history) > 0 {
			if err := marketdata.ValidateBars(a.Symbol, history); err != nil {
				return BacktestResult{}, err
			}
		}
		states[a.Symbol] = &session.SymbolState{Bars: bars, Positions: &account.Position{Symbol: a.Symbol}, AllocationWeight: a.Weight}
	}
	acc := c.InitialAccount
	result := BacktestResult{Snapshots: make([]session.BacktestSnapshot, 0, len(ref)*len(allocations))}
	sessionDate := ""
	for i, reference := range ref {
		date := reference.Timestamp.In(loc).Format(time.DateOnly)
		newSession := date != sessionDate
		sessionDate = date
		fills := map[string][]account.Order{}
		events := map[string][]account.Order{}
		executions := map[string]string{}
		exited := map[string]bool{}
		for _, a := range allocations {
			states[a.Symbol].Positions.CurrentPrice = states[a.Symbol].Bars[i].Open
		}
		acc.Equity = calculateEquity(acc.Cash, allocations, states)
		for _, a := range allocations {
			s := states[a.Symbol]
			b := s.Bars[i]
			p := s.Positions
			if s.PendingOrder != nil {
				order := *s.PendingOrder
				// An entry is good for the next observed consecutive minute only.
				if newSession || !b.Timestamp.Equal(order.CreatedAt) || b.Volume == 0 {
					order.Status = "canceled"
					events[a.Symbol] = append(events[a.Symbol], order)
					executions[a.Symbol] = "entry_canceled_missing_minute_or_liquidity"
				} else if b.Open-(p.EntryPrice-p.StopLossPrice) <= 0 {
					order.Status = "rejected"
					events[a.Symbol] = append(events[a.Symbol], order)
					executions[a.Symbol] = "entry_rejected_invalid_gap_stop"
				} else {
					filled, ok := session.FillPendingOrder(order, p, &acc, b, a.Weight)
					if ok {
						fills[a.Symbol] = append(fills[a.Symbol], filled)
						executions[a.Symbol] = "entry_filled_next_open"
					} else {
						events[a.Symbol] = append(events[a.Symbol], filled)
						executions[a.Symbol] = "entry_rejected_funds_or_allocation"
					}
				}
				s.PendingOrder = nil
				if p.Quantity == 0 {
					p.EntryPrice = 0
					p.StopLossPrice = 0
					p.TakeProfitPrice = 0
				}
			}
		}
		// All opening entries settle before any intraminute proceeds become available.
		for _, a := range allocations {
			s := states[a.Symbol]
			b := s.Bars[i]
			p := s.Positions
			if p.Quantity > 0 && b.Volume > 0 {
				price, reason := protectiveExit(*p, b)
				if price > 0 {
					signal := algorithm.Signal{Symbol: a.Symbol, Action: "Sell", Price: price, CreatedAt: b.Timestamp}
					order := broker.SubmitOrder(signal, p.Quantity)
					executionBar := b
					executionBar.Open = price
					filled, ok := session.FillPendingOrder(order, p, &acc, executionBar, a.Weight)
					if !ok {
						return BacktestResult{}, fmt.Errorf("protective exit failed for %s", a.Symbol)
					}
					fills[a.Symbol] = append(fills[a.Symbol], filled)
					executions[a.Symbol] = reason
					exited[a.Symbol] = true
				}
			} else if p.Quantity > 0 {
				executions[a.Symbol] = "exit_unavailable_zero_volume"
			}
		}
		for _, a := range allocations {
			states[a.Symbol].Positions.CurrentPrice = states[a.Symbol].Bars[i].Close
		}
		acc.Equity = calculateEquity(acc.Cash, allocations, states)
		for _, a := range allocations {
			s := states[a.Symbol]
			b := s.Bars[i]
			p := s.Positions
			previous := s.Indicators
			if newSession {
				s.VWAP = algorithm.VWAPState{}
				previous.VolumeReady = false
			}
			algorithm.VWAP(b, &s.VWAP, &s.Indicators)
			algorithm.StandardDeviation(b, &s.VWAP, &s.Indicators)
			s.Indicators.VolumeReady = s.VWAP.TotalVolume > 0
			if i == 0 {
				s.Indicators.EMA = b.Close
			} else {
				s.Indicators.EMA = previous.EMA + 2.0/21*(b.Close-previous.EMA)
			}
			s.Indicators.EMASlope = s.Indicators.EMA - previous.EMA
			if i == 0 {
				s.Indicators.EMASlope = 0
			}
			s.Indicators.EMAReady = i >= 19
			signal := algorithm.Signal{Symbol: a.Symbol, Action: "Hold", Price: b.Close, VWAP: s.Indicators.VWAP, CreatedAt: b.Timestamp.Add(time.Minute)}
			reason := "inside_bands"
			var submitted *account.Order
			var diagnostic *session.RiskDecision
			switch {
			case exited[a.Symbol]:
				signal.Action = "Sell"
				signal.Price = fills[a.Symbol][len(fills[a.Symbol])-1].FilledPrice
				reason = "protective_exit_no_same_bar_reentry"
			case p.Quantity > 0:
				reason = "position_open"
			case b.Volume == 0:
				reason = "no_volume"
			case s.Indicators.StandardDeviation <= 0:
				reason = "bands_not_ready"
			default:
				touched := previous.VolumeReady && previous.StandardDeviation > 0 && b.Low <= previous.LowerBand
				if touched || b.Close <= s.Indicators.LowerBand {
					signal.Action = "Buy"
					reason = "vwap_dip"
					candidate := *p
					candidate.EntryPrice = b.Close
					candidate.StopLossPrice = b.Close - s.Indicators.StandardDeviation
					candidate.TakeProfitPrice = s.Indicators.VWAP + .5*s.Indicators.StandardDeviation
					local := b.Timestamp.In(loc)
					switch {
					case candidate.StopLossPrice <= 0 || candidate.TakeProfitPrice <= candidate.EntryPrice:
						reason = "buy_rejected_invalid_reward_or_stop"
					case i == len(ref)-1:
						reason = "buy_not_submitted_final_bar"
					case local.Hour() == 15 && local.Minute() == 59:
						reason = "buy_not_submitted_session_close"
					case acc.Cash <= 0 || acc.BuyingPower <= 0:
						reason = "buy_rejected_funds"
					case acc.Equity*a.Weight <= 0:
						reason = "buy_rejected_allocation"
					default:
						input := c.MonteCarloInput
						input.Volatility, err = intradayVolatility(c, a.Symbol, date, loc)
						if err != nil {
							return BacktestResult{}, err
						}
						model := simulation.MonteCarloResult{}
						simulation.GBMModel(input, candidate, &model, signal)
						fraction := risk.KellyCriterion(model, candidate)
						qty := risk.EvaluateRisk(model, candidate, acc, a.Weight)
						diagnostic = &session.RiskDecision{WinProbability: model.WinProbability, LossProbability: model.LossProbability, TimeoutProbability: model.TimeoutProbability, KellyFraction: fraction, Quantity: qty}
						if fraction <= 0 {
							reason = "buy_rejected_kelly"
						} else if qty <= 0 {
							reason = "buy_rejected_sizing"
						} else {
							*p = candidate
							order := broker.SubmitOrder(signal, qty)
							s.PendingOrder = &order
							submitted = &order
							reason = "buy_submitted"
						}
					}
				}
			}
			snapshot := session.BacktestSnapshot{Timestamp: b.Timestamp, Bar: b, Indicator: s.Indicators, Signal: signal, Position: *p, Account: acc, SubmittedOrder: submitted, FilledOrders: fills[a.Symbol], OrderEvents: events[a.Symbol], DecisionReason: reason, ExecutionReason: executions[a.Symbol], Risk: diagnostic}
			if len(snapshot.FilledOrders) > 0 {
				last := snapshot.FilledOrders[len(snapshot.FilledOrders)-1]
				snapshot.FilledOrder = &last
			}
			result.Snapshots = append(result.Snapshots, snapshot)
		}
	}
	result.FinalAccount = acc
	result.FinalPositions = map[string]account.Position{}
	for symbol, s := range states {
		result.FinalPositions[symbol] = *s.Positions
	}
	return result, nil
}

// Opening gaps have known precedence. Otherwise OHLC cannot establish which
// boundary came first, so an ambiguous bar takes the conservative stop exit.
func protectiveExit(p account.Position, b marketdata.BarTick) (float64, string) {
	if b.Open <= p.StopLossPrice {
		return b.Open, "stop_gap_open"
	}
	if b.Open >= p.TakeProfitPrice {
		return b.Open, "target_gap_open"
	}
	if b.Low <= p.StopLossPrice {
		if b.High >= p.TakeProfitPrice {
			return p.StopLossPrice, "both_boundaries_stop_first"
		}
		return p.StopLossPrice, "stop_touch"
	}
	if b.High >= p.TakeProfitPrice {
		return p.TakeProfitPrice, "target_touch"
	}
	return 0, ""
}

// Minute execution does not reinterpret a 20-day estimator as 20 minutes.
// Even though data is prefetched, only completed earlier US sessions enter it.
func intradayVolatility(c BacktestConfig, symbol, date string, loc *time.Location) (float64, error) {
	var prior []marketdata.BarTick
	for _, b := range c.DailyVolatilityBars[symbol] {
		if b.Timestamp.In(loc).Format(time.DateOnly) >= date {
			break
		}
		prior = append(prior, b)
	}
	if len(prior) < 3 {
		return c.MonteCarloInput.Volatility, nil
	}
	prior = prior[max(0, len(prior)-c.VolatilityWindow):]
	return simulation.AnnualizedVolatility(prior, c.PeriodsPerYear)
}
