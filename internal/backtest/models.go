package backtest

import (
	"apexquant/internal/account"
	"apexquant/internal/marketdata"
	"apexquant/internal/session"
	"apexquant/internal/simulation"
)

// BacktestConfig contains everything required to run backtest
// API fetching and terminal input happen before creating this config
type BacktestConfig struct {
	InitialAccount   account.Account                 `json:"initial_account"`
	Allocations      []session.PortfolioAllocation   `json:"allocations"`
	BarsBySymbol     map[string][]marketdata.BarTick `json:"bars_by_symbol"`
	MonteCarloInput  simulation.MonteCarlo           `json:"monte_carlo_input"`
	VolatilityWindow int                             `json:"volatility_window"`
	PeriodsPerYear   float64                         `json:"periods_per_year"`
}

// BacktestResult contains the completed backtest output
type BacktestResult struct {
	FinalAccount   account.Account             `json:"final_account"`
	FinalPositions map[string]account.Position `json:"final_positions"`
	Snapshots      []session.BacktestSnapshot  `json:"snapshots"`
}
