package session

import (
	"time"

	"apexquant/internal/account"
	"apexquant/internal/algorithm"
	"apexquant/internal/marketdata"
)

type BacktestSnapshot struct {
	Timestamp      time.Time
	Bar            marketdata.BarTick
	Indicator      algorithm.Indicator
	Signal         algorithm.Signal
	Position       account.Position
	Account        account.Account
	SubmittedOrder *account.Order
	FilledOrder    *account.Order
	// FilledOrders preserves an entry and protective exit in the same minute.
	FilledOrders    []account.Order
	OrderEvents     []account.Order
	DecisionReason  string
	ExecutionReason string
	Risk            *RiskDecision
}

// RiskDecision explains entry gating without conflating signals with orders.
type RiskDecision struct {
	RiskFreeRate       float64 `json:"risk_free_rate"`
	Model              string  `json:"model"`
	TimeoutMeanReturn  float64 `json:"timeout_mean_return"`
	WinProbability     float64 `json:"win_probability"`
	LossProbability    float64 `json:"loss_probability"`
	TimeoutProbability float64 `json:"timeout_probability"`
	KellyFraction      float64 `json:"kelly_fraction"`
	Quantity           float64 `json:"quantity"`
}
