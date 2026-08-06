package mockdata

import (
	"time"

	"apexquant/internal/account"
	"apexquant/internal/marketdata"
	"apexquant/internal/simulation"
)

var Bars = []marketdata.BarTick{
	{Symbol: "AAPL", Open: 307.36, High: 314.20, Low: 307.00, Close: 312.66, Volume: 53_589_977, Timestamp: time.Date(2026, 7, 6, 20, 0, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 315.29, High: 315.48, Low: 310.15, Close: 310.66, Volume: 42_490_002, Timestamp: time.Date(2026, 7, 7, 20, 0, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 311.91, High: 314.82, Low: 307.05, Close: 313.39, Volume: 41_323_480, Timestamp: time.Date(2026, 7, 8, 20, 0, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 310.51, High: 316.53, Low: 308.16, Close: 316.22, Volume: 48_095_310, Timestamp: time.Date(2026, 7, 9, 20, 0, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 314.72, High: 316.91, Low: 312.17, Close: 315.32, Volume: 30_821_487, Timestamp: time.Date(2026, 7, 10, 20, 0, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 317.02, High: 323.45, Low: 315.78, Close: 317.31, Volume: 43_115_815, Timestamp: time.Date(2026, 7, 13, 20, 0, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 313.76, High: 316.19, Low: 311.91, Close: 314.86, Volume: 34_502_128, Timestamp: time.Date(2026, 7, 14, 20, 0, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 317.62, High: 328.73, Low: 317.32, Close: 327.50, Volume: 60_659_338, Timestamp: time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 328.01, High: 334.68, Low: 326.79, Close: 333.26, Volume: 62_612_109, Timestamp: time.Date(2026, 7, 16, 20, 0, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 331.98, High: 334.99, Low: 329.00, Close: 333.74, Volume: 63_237_812, Timestamp: time.Date(2026, 7, 17, 20, 0, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 333.51, High: 333.71, Low: 323.68, Close: 326.59, Volume: 53_042_385, Timestamp: time.Date(2026, 7, 20, 20, 0, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 323.13, High: 329.60, Low: 322.22, Close: 327.74, Volume: 40_991_498, Timestamp: time.Date(2026, 7, 21, 20, 0, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 327.87, High: 329.00, Low: 323.34, Close: 325.89, Volume: 38_558_401, Timestamp: time.Date(2026, 7, 22, 20, 0, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 321.73, High: 323.30, Low: 319.35, Close: 321.66, Volume: 39_822_216, Timestamp: time.Date(2026, 7, 23, 20, 0, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 321.79, High: 334.37, Low: 321.62, Close: 333.02, Volume: 47_251_897, Timestamp: time.Date(2026, 7, 24, 20, 0, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 334.54, High: 339.57, Low: 334.02, Close: 336.91, Volume: 49_246_821, Timestamp: time.Date(2026, 7, 27, 20, 0, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 340.03, High: 342.89, Low: 335.60, Close: 340.08, Volume: 50_773_264, Timestamp: time.Date(2026, 7, 28, 20, 0, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 339.73, High: 344.57, Low: 337.35, Close: 338.19, Volume: 55_279_473, Timestamp: time.Date(2026, 7, 29, 20, 0, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 333.10, High: 334.75, Low: 329.59, Close: 333.43, Volume: 60_837_821, Timestamp: time.Date(2026, 7, 30, 20, 0, 0, 0, time.UTC)},
	{Symbol: "AAPL", Open: 304.81, High: 310.69, Low: 300.00, Close: 308.91, Volume: 131_614_915, Timestamp: time.Date(2026, 7, 31, 20, 0, 0, 0, time.UTC)},
}

// Something cause the signal Buy but it didnt buy
var InitialAccount = account.Account{
	Equity:      50_000,
	Cash:        50_000,
	BuyingPower: 50_000, // Will add fee in transaction, thus, dock 500$ manually first
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
