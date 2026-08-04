package session

import (
	"time"

	"apexquant/internal/account"
	"apexquant/internal/algorithm"
	"apexquant/internal/broker"
	"apexquant/internal/marketdata"
	"apexquant/internal/risk"
	"apexquant/internal/simulation"
)

// SymbolState stores the current state of the selected symbol
type SymbolState struct {
	Bars       []marketdata.BarTick `json:"bars"`
	Indicators algorithm.Indicator  `json:"indicators"`
	VWAP       algorithm.VWAPState  `json:"vwap"`
	Positions  *account.Position    `json:"positions"`
}

type TradingSession struct {
	ID              string                  `json:"id"`
	StartingCapital float64                 `json:"starting_capital"`
	Cash            float64                 `json:"cash"`
	MaxDrawdown     float64                 `json:"max_drawdown"`
	Symbols         map[string]*SymbolState `json:"symbols"`
	Order           []account.Order         `json:"orders"`
	Running         bool                    `json:"running"`
	StartedAt       time.Time               `json:"started_at"`
}

// Current flow for a signal
func Session(signal algorithm.Signal, input simulation.MonteCarlo, position account.Position, result simulation.MonteCarloResult, acc account.Account) {
	switch signal.Action {
	case "Buy":
		simulation.GBMModel(input, position, &result)
		quantity := risk.EvaluateRisk(result, position, acc)
		broker.SubmitOrder(signal, quantity)
	case "Sell":
		broker.SubmitOrder(signal, position.Quantity)
	default:
		return
	}
}
