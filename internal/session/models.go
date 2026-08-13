package session

import (
	"fmt"
	"math"
	"strings"
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
	Bars             []marketdata.BarTick `json:"bars"`
	Indicators       algorithm.Indicator  `json:"indicators"`
	VWAP             algorithm.VWAPState  `json:"vwap"`
	Positions        *account.Position    `json:"positions"`
	AllocationWeight float64              `json:"allocation_weight"`
	PendingOrder     *account.Order       `json:"pending_order"`
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

type PortfolioAllocation struct {
	Symbol string  `json:"symbol"`
	Weight float64 `json:"weight"`
}

// Current flow for a signal
func Session(signal algorithm.Signal, input simulation.MonteCarlo, position *account.Position, result simulation.MonteCarloResult, acc *account.Account, order *account.Order, allocationWeight float64) (account.Order, bool) {

	switch signal.Action {
	case "Buy":

		simulation.GBMModel(input, *position, &result, signal)
		order.Quantity = risk.EvaluateRisk(result, *position, *acc, allocationWeight)

		if order.Quantity <= 0 {

			fmt.Printf("\n\tOrder Quantity Less than or Equal to 0: %v\n", order.Quantity)
			return account.Order{}, false
		}
		submittedOrder := broker.SubmitOrder(signal, order.Quantity)

		return submittedOrder, true

	case "Sell":

		if position.Quantity == 0 {
			return account.Order{}, false
		}
		submittedOrder := broker.SubmitOrder(signal, position.Quantity)
		return submittedOrder, true

	default:
		return account.Order{}, false
	}
}

func PortfolioAllocate(tickers []string, weights []float64, allocate *[]PortfolioAllocation) error {
	if len(tickers) < 1 || len(tickers) > 8 {
		return fmt.Errorf("number of tickers must be between 1 and 8")
	}

	if len(tickers) != len(weights) {
		return fmt.Errorf("each ticker must have one weight")
	}

	totalWeight := 0.0
	*allocate = make([]PortfolioAllocation, 0, len(tickers))

	for i := range tickers {
		if weights[i] <= 0 {
			return fmt.Errorf("%s has an invalid weight", tickers[i])
		}

		totalWeight += weights[i]

		*allocate = append(*allocate, PortfolioAllocation{
			Symbol: strings.ToUpper(strings.TrimSpace(tickers[i])),
			Weight: weights[i],
		})
	}

	if math.Abs(totalWeight-1.0) > 0.000001 {
		return fmt.Errorf("total weight must equal 1.0")
	}

	return nil
}

func FillPendingOrder(pendingOrder account.Order, position *account.Position, acc *account.Account, bar marketdata.BarTick, allocationWeight float64) (account.Order, bool) {

	filledOrder := broker.FillOrderAtNextBar(pendingOrder, bar)

	if filledOrder.Status != "filled" {
		return filledOrder, false
	}

	switch filledOrder.Action {
	case "Buy":
		availableFunds := min(acc.Cash, acc.BuyingPower)

		remainingAllocation := math.Max(0, acc.Equity*allocationWeight-position.Quantity*filledOrder.FilledPrice)

		maxFundsQuantity := availableFunds / filledOrder.FilledPrice

		maxAllocationQuantity := remainingAllocation / filledOrder.FilledPrice

		filledOrder.Quantity = min(filledOrder.Quantity, maxFundsQuantity, maxAllocationQuantity)

		if filledOrder.Quantity <= 0 {
			filledOrder.Status = "Rejected"
			return filledOrder, false
		}

		// Preserve the stop-loss and target distances.
		stopDistance := position.EntryPrice - position.StopLossPrice

		targetDistance := position.TakeProfitPrice - position.EntryPrice

		position.EntryPrice = filledOrder.FilledPrice
		position.StopLossPrice = filledOrder.FilledPrice - stopDistance

		position.TakeProfitPrice = filledOrder.FilledPrice + targetDistance

		position.Quantity += filledOrder.Quantity

		cost := filledOrder.Quantity * filledOrder.FilledPrice

		acc.Cash -= cost
		acc.BuyingPower -= cost

	case "Sell":
		filledOrder.Quantity = min(filledOrder.Quantity, position.Quantity)

		proceeds := filledOrder.Quantity * filledOrder.FilledPrice

		position.Quantity -= filledOrder.Quantity
		acc.Cash += proceeds
		acc.BuyingPower += proceeds

		if position.Quantity <= 0 {
			position.Quantity = 0
			position.EntryPrice = 0
			position.StopLossPrice = 0
			position.TakeProfitPrice = 0
		}
	}

	return filledOrder, true
}
