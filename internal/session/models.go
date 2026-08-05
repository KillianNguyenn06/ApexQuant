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
func Session(signal algorithm.Signal, input simulation.MonteCarlo, position *account.Position, result simulation.MonteCarloResult, acc *account.Account, order *account.Order, nextBar marketdata.BarTick) (account.Order, bool) {

	switch signal.Action {
	case "Buy":
		simulation.GBMModel(input, *position, &result, signal)
		order.Quantity = risk.EvaluateRisk(result, *position, *acc)
		if order.Quantity <= 0 {
			return account.Order{}, false
		} else {
			order := broker.SubmitOrder(signal, order.Quantity) // Capture order and return order + a successful signal
			order = broker.FillOrderAtNextBar(order, nextBar)
			position.Quantity += order.Quantity
			acc.BuyingPower -= order.Quantity * position.EntryPrice
			acc.Cash -= order.Quantity * position.EntryPrice
			acc.Equity = acc.Cash + order.Quantity*position.EntryPrice
			return order, true
		}
	case "Sell":

		order := broker.SubmitOrder(signal, position.Quantity)
		order = broker.FillOrderAtNextBar(order, nextBar)
		position.Quantity -= order.Quantity
		acc.BuyingPower += order.Quantity * position.CurrentPrice
		acc.Cash += order.Quantity * position.EntryPrice
		acc.Equity = acc.Cash - order.Quantity*position.EntryPrice
		return order, true

	default:
		return account.Order{}, false
	}
}
