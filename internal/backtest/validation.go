package backtest

import (
	"apexquant/internal/marketdata"
	"apexquant/internal/session"
	"fmt"
	"math"
	"strings"
	"time"
)

func validateConfig(config BacktestConfig) ([]session.PortfolioAllocation, error) {
	if config.HoldingDays < 0 || config.HoldingDays > 252 {
		return nil, fmt.Errorf("holding days must be 1 to 252, or omitted")
	}
	m := config.MonteCarloInput
	if m.Model == "vwap_reversion_auto" && (config.Timeframe == "1Min" || m.ReversionHalfLifeDays != 0) {
		return nil, fmt.Errorf("auto reversion requires daily bars and no manual override")
	}
	if m.Model != "" && m.Model != "gbm" && m.Model != "vwap_reversion" && m.Model != "vwap_reversion_auto" {
		return nil, fmt.Errorf("unknown probability model")
	}
	if math.IsNaN(m.ReversionHalfLifeDays) || math.IsInf(m.ReversionHalfLifeDays, 0) || m.ReversionHalfLifeDays < 0 || m.ReversionHalfLifeDays > 252 || (m.Model == "vwap_reversion" && m.ReversionHalfLifeDays <= 0) {
		return nil, fmt.Errorf("invalid reversion half life")
	}
	for _, r := range config.RiskFreeRates {
		date, e1 := time.Parse(time.DateOnly, r.Date)
		available, e2 := time.Parse(time.DateOnly, r.AvailableDate)
		if e1 != nil || e2 != nil || !available.After(date) || math.IsNaN(r.Rate) || math.IsInf(r.Rate, 0) {
			return nil, fmt.Errorf("invalid historical rate")
		}
	}
	if config.DailyVWAPWindow != 0 && (config.Timeframe == "1Min" || config.DailyVWAPWindow < 2 || config.DailyVWAPWindow > 252) {
		return nil, fmt.Errorf("daily VWAP window must be 2 to 252 trading bars and applies only to daily execution")
	}
	for _, value := range []float64{config.InitialAccount.Cash, config.InitialAccount.Equity, config.InitialAccount.BuyingPower, config.PeriodsPerYear, config.MonteCarloInput.TimeHorizon, config.MonteCarloInput.Volatility, config.MonteCarloInput.RiskFreeRate} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("configuration values must be finite")
		}
	}
	if config.InitialAccount.Cash <= 0 || config.InitialAccount.BuyingPower < 0 || config.InitialAccount.Equity != config.InitialAccount.Cash {
		return nil, fmt.Errorf("initial account must have positive cash, matching equity, and nonnegative buying power")
	}
	if config.MonteCarloInput.Volatility < 0 {
		return nil, fmt.Errorf("volatility cannot be negative")
	}
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
		allocation.Symbol = strings.ToUpper(strings.TrimSpace(allocation.Symbol))
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

		if err := marketdata.ValidateBarsTimeframe(allocation.Symbol, bars, config.Timeframe); err != nil {
			return nil, err
		}
		history := config.VolatilityHistory[allocation.Symbol]
		historyLimit := config.VolatilityWindow
		if m.Model == "vwap_reversion_auto" {
			historyLimit = 253 + DailyVWAPWindowSize(config)
		}
		if len(history) > historyLimit {
			return nil, fmt.Errorf("too many volatility history bars for %s", allocation.Symbol)
		}
		if len(history) > 0 {
			if err := marketdata.ValidateBars(allocation.Symbol, history); err != nil {
				return nil, err
			}
			if history[len(history)-1].Timestamp.UTC().Format("2006-01-02") >= bars[0].Timestamp.UTC().Format("2006-01-02") {
				return nil, fmt.Errorf("volatility history must precede trading for %s", allocation.Symbol)
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
			referenceDate := referenceBars[i].Timestamp.UTC().Format(
				"2006-01-02",
			)
			symbolDate := bars[i].Timestamp.UTC().Format(
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
