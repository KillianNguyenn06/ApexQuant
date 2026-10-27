package backtest

import (
	"apexquant/internal/account"
	"apexquant/internal/marketdata"
	"apexquant/internal/session"
	"apexquant/internal/simulation"
)

// MinuteExecutionEnabled gates application entry points while storage and compute
// work is pending. The minute engine remains available to deterministic tests.
const MinuteExecutionEnabled = false

// BacktestConfig contains everything required to run backtest
// API fetching and terminal input happen before creating this config
type BacktestConfig struct {
	ReversionEstimates  map[string]simulation.ReversionEstimate `json:"reversion_estimates,omitempty"`
	InitialRateDate     string                                  `json:"initial_rate_date,omitempty"`
	HoldingDays         int                                     `json:"holding_days"`
	RiskFreeRates       []marketdata.RateObservation            `json:"risk_free_rates,omitempty"`
	RatePolicy          string                                  `json:"rate_policy,omitempty"`
	DailyVWAPWindow     int                                     `json:"daily_vwap_window,omitempty"`
	DailyVolatilityBars map[string][]marketdata.BarTick         `json:"daily_volatility_bars,omitempty"`
	Timeframe           string                                  `json:"timeframe,omitempty"`
	InitialAccount      account.Account                         `json:"initial_account"`
	Allocations         []session.PortfolioAllocation           `json:"allocations"`
	BarsBySymbol        map[string][]marketdata.BarTick         `json:"bars_by_symbol"`
	// VolatilityHistory contains only bars before trading begins, per symbol.
	VolatilityHistory map[string][]marketdata.BarTick `json:"volatility_history,omitempty"`
	MonteCarloInput   simulation.MonteCarlo           `json:"monte_carlo_input"`
	VolatilityWindow  int                             `json:"volatility_window"`
	PeriodsPerYear    float64                         `json:"periods_per_year"`
}

// BacktestResult contains the completed backtest output
type BacktestResult struct {
	FinalAccount   account.Account             `json:"final_account"`
	FinalPositions map[string]account.Position `json:"final_positions"`
	Snapshots      []session.BacktestSnapshot  `json:"snapshots"`
}

// DailyVWAPWindowSize makes omitted engine settings use the public daily default.
func DailyVWAPWindowSize(config BacktestConfig) int {
	if config.Timeframe == "1Min" {
		return 0
	}
	if config.DailyVWAPWindow == 0 {
		return 20
	}
	return config.DailyVWAPWindow
}

// HoldingPeriod uses a bounded trading-bar deadline, including the entry day.
func HoldingPeriod(c BacktestConfig) int {
	if c.HoldingDays == 0 {
		return 5
	}
	return c.HoldingDays
}
