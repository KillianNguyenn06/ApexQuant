package backtest

import (
	"apexquant/internal/session"
	"fmt"
)

func validateConfig(config BacktestConfig) ([]session.PortfolioAllocation, error) {
	if config.VolatilityWindow < 3 {
		return nil, fmt.Errorf("volatility window must be at least 3")
	}

	if config.PeriodsPerYear <= 0 {
		return nil, fmt.Errorf("periods per year must be greater than zero")
	}

	if config.MonteCarloInput.NumPaths <= 0 {
		return nil, fmt.Errorf("Monte Carlo paths must be greater than zero")
	}

	if config.MonteCarloInput.NumSteps <= 0 {
		return nil, fmt.Errorf("Monte Carlo steps must be greater than zero")
	}

	if config.MonteCarloInput.TimeHorizon <= 0 {
		return nil, fmt.Errorf("Monte Carlo time horizon must be greater than zero")
	}

	tickers := make([]string, 0, len(config.Allocations))
	weights := make([]float64, 0, len(config.Allocations))
	usedSymbols := make(map[string]bool)

	for _, allocation := range config.Allocations {
		if usedSymbols[allocation.Symbol] {
			return nil, fmt.Errorf("duplicated symbol: %s", allocation.Symbol)
		}
		usedSymbols[allocation.Symbol] = true
		tickers = append(tickers, allocation.Symbol)
		weights = append(weights, allocation.Weight)
	}

	var allocations []session.PortfolioAllocation

	if err := session.PortfolioAllocate(tickers, weights, &allocations); err != nil {
		return nil, err
	}

	for _, allocation := range allocations {
		bars, exists := config.BarsBySymbol[allocation.Symbol]

		if !exists {
			return nil, fmt.Errorf("bars are missing for %s", allocation.Symbol)
		}

		if len(bars) < 2 {
			return nil, fmt.Errorf("%s requires at least two bars", allocation.Symbol)
		}

		for i, bar := range bars {
			if bar.Symbol != allocation.Symbol {
				return nil, fmt.Errorf("%s bar %d contains symbol %s", allocation.Symbol, i, bar.Symbol)
			}
		}
	}
	return allocations, nil
}

func validateBarTimeline(allocations []session.PortfolioAllocation, symbols map[string]*session.SymbolState) error {
	referenceSymbol := allocations[0].Symbol
	referenceBars := symbols[referenceSymbol].Bars
	for _, allocation := range allocations[1:] {
		symbol := allocation.Symbol
		bars := symbols[symbol].Bars

		if len(bars) != len(referenceBars) {
			return fmt.Errorf(
				"bar count mismatch: %s has %d bars, %s has %d bars",
				referenceSymbol,
				len(referenceBars),
				symbol,
				len(bars),
			)
		}

		for i := range referenceBars {
			referenceDate := referenceBars[i].Timestamp.Format(
				"2006-01-02",
			)
			symbolDate := bars[i].Timestamp.Format(
				"2006-01-02",
			)

			if referenceDate != symbolDate {
				return fmt.Errorf(
					"bar date mismatch at index %d: %s=%s, %s=%s",
					i,
					referenceSymbol,
					referenceDate,
					symbol,
					symbolDate,
				)
			}
		}
	}

	return nil
}
