package backtest

import (
	"apexquant/internal/account"
	"apexquant/internal/algorithm"
	"apexquant/internal/session"
	"apexquant/internal/simulation"
	"fmt"
)

func RunBacktest(config BacktestConfig) (BacktestResult, error) {
	allocations, err := validateConfig(config)
	if err != nil {
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
	monteCarloInput := config.MonteCarloInput

	referenceSymbol := allocations[0].Symbol
	barCount := len(symbols[referenceSymbol].Bars)

	snapshots := make(
		[]session.BacktestSnapshot,
		0,
		barCount*len(allocations),
	)

	for i := 0; i < barCount; i++ {
		filledOrders := make(map[string]*account.Order)

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

			filledOrder, filled := session.FillPendingOrder(*state.PendingOrder, state.Positions, &acc, state.Bars[i], state.AllocationWeight)
			if filled {
				orderCopy := filledOrder
				filledOrders[allocation.Symbol] = &orderCopy
			}

			state.PendingOrder = nil
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

			startIndex := max(
				0,
				i-config.VolatilityWindow+1,
			)

			availableBars := state.Bars[startIndex : i+1]

			if len(availableBars) >= 3 {
				volatility, err := simulation.AnnualizedVolatility(
					availableBars,
					config.PeriodsPerYear,
				)
				if err != nil {
					return BacktestResult{}, fmt.Errorf(
						"%s volatility: %w",
						allocation.Symbol,
						err,
					)
				}

				monteCarloInput.Volatility = volatility
			}

			algorithm.VWAP(bar, &state.VWAP, &state.Indicators)
			algorithm.StandardDeviation(bar, &state.VWAP, &state.Indicators)
			signal := algorithm.DecisionMaking(&state.Indicators, state.Positions, bar)

			var submittedOrder *account.Order

			// The last bar can fill an order but cannot submit one.
			if i < barCount-1 {
				order, submitted := session.Session(signal, monteCarloInput, state.Positions, simulation.MonteCarloResult{}, &acc, &account.Order{}, state.AllocationWeight)

				if submitted {
					orderCopy := order
					state.PendingOrder = &orderCopy
					submittedOrder = &orderCopy
				}
			}

			snapshots = append(snapshots, session.BacktestSnapshot{
				Timestamp:      bar.Timestamp,
				Bar:            bar,
				Indicator:      state.Indicators,
				Signal:         signal,
				Position:       *state.Positions,
				Account:        acc,
				SubmittedOrder: submittedOrder,
				FilledOrder:    filledOrders[allocation.Symbol],
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

func calculateEquity(cash float64, allocations []session.PortfolioAllocation, symbols map[string]*session.SymbolState) float64 {
	equity := cash

	for _, allocation := range allocations {
		position := symbols[allocation.Symbol].Positions
		equity += position.Quantity * position.CurrentPrice
	}
	return equity
}
