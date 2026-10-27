package backtest

import (
	"apexquant/internal/account"
	"apexquant/internal/algorithm"
	"apexquant/internal/broker"
	"apexquant/internal/marketdata"
	"apexquant/internal/session"
	"apexquant/internal/simulation"
	"fmt"
	"time"
)

func RunBacktest(config BacktestConfig) (BacktestResult, error) {
	allocations, err := validateConfig(config)
	if err != nil {
		return BacktestResult{}, err
	}

	if config.Timeframe == "1Min" {
		return runIntraday(config, allocations)
	}

	config.Allocations = allocations
	if err := prepareReversion(&config); err != nil {
		return BacktestResult{}, err
	}
	symbols := make(map[string]*session.SymbolState, len(allocations))

	for _, allocation := range allocations {
		bars := config.BarsBySymbol[allocation.Symbol]

		position := account.Position{
			Symbol: allocation.Symbol,
		}

		symbols[allocation.Symbol] = &session.SymbolState{
			Bars:             bars,
			Positions:        &position,
			AllocationWeight: allocation.Weight,
		}
	}
	if err := validateBarTimeline(allocations, symbols); err != nil {
		return BacktestResult{}, err
	}

	acc := config.InitialAccount

	referenceSymbol := allocations[0].Symbol
	barCount := len(symbols[referenceSymbol].Bars)

	snapshots := make(
		[]session.BacktestSnapshot,
		0,
		barCount*len(allocations),
	)

	entryBars := make(map[string]int)
	for i := 0; i < barCount; i++ {
		filledOrders := make(map[string][]account.Order)
		exited := make(map[string]bool)
		orderEvents := make(map[string][]account.Order)
		executionReasons := make(map[string]string)

		// 1. Mark every position at the current opening price
		for _, allocation := range allocations {
			state := symbols[allocation.Symbol]
			state.Positions.CurrentPrice = state.Bars[i].Open
		}

		// 2. Calculate opening porfolio equity
		acc.Equity = calculateEquity(acc.Cash, allocations, symbols)

		// 3. Fill orders submitted during the previous bar
		for _, allocation := range allocations {
			state := symbols[allocation.Symbol]

			if state.PendingOrder == nil {
				continue
			}

			pending := *state.PendingOrder
			bar := state.Bars[i]
			var filledOrder account.Order
			filled := false
			switch {
			case bar.Volume <= 0:
				filledOrder = pending
				filledOrder.Status = "canceled"
				executionReasons[allocation.Symbol] = "entry_canceled_no_volume"
			case bar.Open-(state.Positions.EntryPrice-state.Positions.StopLossPrice) <= 0:
				filledOrder = pending
				filledOrder.Status = "rejected"
				executionReasons[allocation.Symbol] = "entry_rejected_invalid_gap_stop"
			default:
				filledOrder, filled = session.FillPendingOrder(pending, state.Positions, &acc, bar, state.AllocationWeight)
				executionReasons[allocation.Symbol] = "order_rejected_at_open_funds_or_allocation"
				if !filled {
					filledOrder.Status = "rejected"
				}
			}
			if filled {
				entryBars[allocation.Symbol] = i
				filledOrders[allocation.Symbol] = append(filledOrders[allocation.Symbol], filledOrder)
				executionReasons[allocation.Symbol] = "entry_filled_next_open"
				if filledOrder.Quantity < pending.Quantity {
					executionReasons[allocation.Symbol] = "entry_filled_reduced_by_funds_or_allocation"
				}
			} else {
				orderEvents[allocation.Symbol] = append(orderEvents[allocation.Symbol], filledOrder)
				state.Positions.EntryPrice = 0
				state.Positions.StopLossPrice = 0
				state.Positions.TakeProfitPrice = 0
			}

			state.PendingOrder = nil
		}
		// Entries at the open settle before proceeds from any later touch.
		for _, allocation := range allocations {
			state := symbols[allocation.Symbol]
			bar := state.Bars[i]
			p := state.Positions
			if p.Quantity <= 0 {
				continue
			}
			if bar.Volume <= 0 {
				executionReasons[allocation.Symbol] = "exit_unavailable_zero_volume"
				continue
			}
			held := i - entryBars[allocation.Symbol] + 1
			price, reason := 0.0, ""
			if held > HoldingPeriod(config) && bar.Open > p.StopLossPrice && bar.Open < p.TakeProfitPrice {
				// An overdue market exit executes at open before any later wick.
				price, reason = bar.Open, "holding_expiry_delayed_open"
			} else {
				price, reason = protectiveExit(*p, bar)
				if price <= 0 && held >= HoldingPeriod(config) {
					price, reason = bar.Close, "holding_expiry_close"
				}
			}
			if price <= 0 {
				continue
			}
			order := broker.SubmitOrder(algorithm.Signal{Symbol: allocation.Symbol, Action: "Sell", Price: price, CreatedAt: bar.Timestamp}, p.Quantity)
			executionBar := bar
			executionBar.Open = price
			filled, ok := session.FillPendingOrder(order, p, &acc, executionBar, state.AllocationWeight)
			if !ok {
				return BacktestResult{}, fmt.Errorf("daily protective exit failed for %s", allocation.Symbol)
			}
			filledOrders[allocation.Symbol] = append(filledOrders[allocation.Symbol], filled)
			executionReasons[allocation.Symbol] = reason
			exited[allocation.Symbol] = true
		}
		// 4. Mark every position at the current closing price.
		for _, allocation := range allocations {
			state := symbols[allocation.Symbol]
			state.Positions.CurrentPrice = state.Bars[i].Close
		}

		// 5. Calculate closing portfolio equity.
		acc.Equity = calculateEquity(acc.Cash, allocations, symbols)

		// 6. Calculate indicators, signals and new orders.
		for _, allocation := range allocations {
			state := symbols[allocation.Symbol]
			bar := state.Bars[i]
			monteCarloInput := config.MonteCarloInput
			if fit, ok := config.ReversionEstimates[allocation.Symbol]; ok && monteCarloInput.Model == "vwap_reversion_auto" {
				monteCarloInput.ReversionHalfLifeDays = fit.HalfLifeDays
				monteCarloInput.AutoLogDrift = fit.AnnualLogDrift
			}
			monteCarloInput.HoldingDays = HoldingPeriod(config)
			monteCarloInput.TimeHorizon = float64(monteCarloInput.HoldingDays) / config.PeriodsPerYear
			if len(config.RiskFreeRates) > 0 {
				location, _ := time.LoadLocation("America/New_York")
				observation, ok := marketdata.RateAt(config.RiskFreeRates, bar.Timestamp.In(location).Format(time.DateOnly))
				if !ok {
					return BacktestResult{}, fmt.Errorf("historical rate unavailable for decision")
				}
				monteCarloInput.RiskFreeRate = observation.Rate
			}
			monteCarloInput.Volatility, err = volatilityAt(config, allocation.Symbol, i)
			if err != nil {
				return BacktestResult{}, fmt.Errorf("%s volatility: %w", allocation.Symbol, err)
			}

			previous := state.Indicators
			window := DailyVWAPWindowSize(config)
			algorithm.RollingVWAP(state.Bars[max(0, i+1-window):i+1], &state.Indicators)
			var signal algorithm.Signal
			candidate := *state.Positions
			var submittedOrder *account.Order
			var diagnostic *session.RiskDecision
			var reason string
			if !exited[allocation.Symbol] {
				signal, candidate, submittedOrder, diagnostic, reason = dailyDecision(state.Indicators, previous, *state.Positions, bar, monteCarloInput, acc, state.AllocationWeight, i == barCount-1)
			}
			if exited[allocation.Symbol] {
				lastFill := filledOrders[allocation.Symbol][len(filledOrders[allocation.Symbol])-1]
				signal = algorithm.Signal{Symbol: allocation.Symbol, Action: "Sell", Price: lastFill.FilledPrice, CreatedAt: bar.Timestamp}
				candidate = *state.Positions
				submittedOrder = nil
				diagnostic = nil
				reason = "protective_exit_no_same_bar_reentry"
				if executionReasons[allocation.Symbol] == "holding_expiry_close" || executionReasons[allocation.Symbol] == "holding_expiry_delayed_open" {
					reason = "holding_expiry_no_same_bar_reentry"
				}
			}
			if submittedOrder != nil {
				state.PendingOrder = submittedOrder
				*state.Positions = candidate
			}
			var lastFilled *account.Order
			if list := filledOrders[allocation.Symbol]; len(list) > 0 {
				copy := list[len(list)-1]
				lastFilled = &copy
			}

			snapshots = append(snapshots, session.BacktestSnapshot{
				Timestamp:      bar.Timestamp,
				DecisionReason: reason, ExecutionReason: executionReasons[allocation.Symbol], OrderEvents: orderEvents[allocation.Symbol], Risk: diagnostic,
				Bar:            bar,
				Indicator:      state.Indicators,
				Signal:         signal,
				Position:       *state.Positions,
				Account:        acc,
				SubmittedOrder: submittedOrder,
				FilledOrders:   filledOrders[allocation.Symbol], FilledOrder: lastFilled,
			},
			)
		}
	}

	finalPositions := make(map[string]account.Position, len(symbols))

	for symbol, state := range symbols {
		finalPositions[symbol] = *state.Positions
	}

	return BacktestResult{
		FinalAccount:   acc,
		FinalPositions: finalPositions,
		Snapshots:      snapshots,
	}, nil

}

// volatilityAt uses only prior history and trading bars through the current close.
// Preparation bars never enter indicator, order, account, or replay processing.
func volatilityAt(config BacktestConfig, symbol string, index int) (float64, error) {
	trading := config.BarsBySymbol[symbol][max(0, index-config.VolatilityWindow+1) : index+1]
	needed := config.VolatilityWindow - len(trading)
	history := config.VolatilityHistory[symbol]
	if len(history) > needed {
		history = history[len(history)-needed:]
	}
	bars := make([]marketdata.BarTick, 0, len(history)+len(trading))
	bars = append(bars, history...)
	bars = append(bars, trading...)
	if len(bars) < 3 {
		return config.MonteCarloInput.Volatility, nil
	}
	return simulation.AnnualizedVolatility(bars, config.PeriodsPerYear)
}

func calculateEquity(cash float64, allocations []session.PortfolioAllocation, symbols map[string]*session.SymbolState) float64 {
	equity := cash

	for _, allocation := range allocations {
		position := symbols[allocation.Symbol].Positions
		equity += position.Quantity * position.CurrentPrice
	}
	return equity
}

// Calibrate before trading starts. Future trading bars can never influence the fit.
func prepareReversion(c *BacktestConfig) error {
	if c.MonteCarloInput.Model != "vwap_reversion_auto" {
		return nil
	}
	if c.Timeframe == "1Min" {
		return fmt.Errorf("auto reversion requires daily bars")
	}
	c.ReversionEstimates = make(map[string]simulation.ReversionEstimate)
	for _, a := range c.Allocations {
		history := c.VolatilityHistory[a.Symbol]
		trading := c.BarsBySymbol[a.Symbol]
		if len(history) == 0 || len(trading) == 0 {
			return fmt.Errorf("unreliable reversion estimate for %s", a.Symbol)
		}
		for _, b := range history {
			if !b.Timestamp.Before(trading[0].Timestamp) {
				return fmt.Errorf("auto reversion history overlaps trading")
			}
		}
		fit, err := simulation.EstimateReversion(history, DailyVWAPWindowSize(*c), c.PeriodsPerYear)
		if err != nil {
			return fmt.Errorf("unreliable reversion estimate for %s", a.Symbol)
		}
		c.ReversionEstimates[a.Symbol] = fit
	}
	return nil
}
