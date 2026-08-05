package mockdata

import (
	"time"

	"apexquant/internal/account"
	"apexquant/internal/marketdata"
	"apexquant/internal/simulation"
)

var Bars = []marketdata.BarTick{
	{Symbol: "AAPL", Open: 100.00, High: 100.80, Low: 99.99, Close: 100.40, Volume: 1200, Timestamp: time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 100.40, High: 101.00, Low: 100.10, Close: 100.70, Volume: 1350, Timestamp: time.Date(2026, 1, 2, 14, 31, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 100.70, High: 101.20, Low: 100.30, Close: 100.90, Volume: 1100, Timestamp: time.Date(2026, 1, 2, 14, 32, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 100.90, High: 101.10, Low: 100.20, Close: 100.50, Volume: 1500, Timestamp: time.Date(2026, 1, 2, 14, 33, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 100.50, High: 100.90, Low: 100.20, Close: 100.20, Volume: 1250, Timestamp: time.Date(2026, 1, 2, 14, 34, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 100.20, High: 100.60, Low: 99.80, Close: 100.10, Volume: 1400, Timestamp: time.Date(2026, 1, 2, 14, 35, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 100.10, High: 100.70, Low: 99.90, Close: 100.50, Volume: 1300, Timestamp: time.Date(2026, 1, 2, 14, 36, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 100.50, High: 100.80, Low: 100.00, Close: 100.30, Volume: 1450, Timestamp: time.Date(2026, 1, 2, 14, 37, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 100.30, High: 100.60, Low: 99.70, Close: 100.20, Volume: 1600, Timestamp: time.Date(2026, 1, 2, 14, 38, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 100.20, High: 100.40, Low: 99.80, Close: 100.10, Volume: 1750, Timestamp: time.Date(2026, 1, 2, 14, 39, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 100.10, High: 100.20, Low: 99.40, Close: 99.50, Volume: 2000, Timestamp: time.Date(2026, 1, 2, 14, 40, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 99.55, High: 100.10, Low: 99.30, Close: 99.90, Volume: 2200, Timestamp: time.Date(2026, 1, 2, 14, 41, 0, 0, time.UTC)},
}

var InitialAccount = account.Account{
	Equity:      10_000,
	Cash:        10_000,
	BuyingPower: 9_500, // Will add fee in transaction, thus, dock 500$ manually first
}

var InitialPosition = account.Position{
	Symbol:   "AAPL",
	Quantity: 0,
}

var MonteCarloInput = simulation.MonteCarlo{
	Volatility:   0.20,
	RiskFreeRate: 0.04,
	TimeHorizon:  5.0 / 252.0,
	NumPaths:     100_000,
	NumSteps:     252, // Time = 5 / 252 represent 5 trading day/simulation, steps = 5 represent 1 steps/day
}
