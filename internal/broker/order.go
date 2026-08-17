package broker

import (
	"apexquant/internal/account"
	"apexquant/internal/algorithm"
	"apexquant/internal/marketdata"

	"github.com/google/uuid"
)

// =================================================
// Submit Order using signals from DecisionMaking()
// =================================================
func SubmitOrder(signal algorithm.Signal, quantity float64) account.Order {
	idV4 := uuid.New()
	switch signal.Action {
	case "Buy":
		return account.Order{
			ID:        idV4.String(),
			Symbol:    signal.Symbol,
			Action:    signal.Action,
			Quantity:  quantity,
			Status:    "Submitted",
			CreatedAt: signal.CreatedAt,
		}
	case "Sell":
		return account.Order{
			ID:        idV4.String(),
			Symbol:    signal.Symbol,
			Action:    signal.Action,
			Quantity:  quantity,
			Status:    "Submitted",
			CreatedAt: signal.CreatedAt,
		}
	case "Hold":
		return account.Order{}
	}

	return account.Order{}
}

// =================================================
// Fill Order at Next Opening bar
// =================================================
func FillOrderAtNextBar(order account.Order, nextBar marketdata.BarTick) account.Order {
	if order.Symbol != nextBar.Symbol {
		return order
	}
	order.FilledPrice = nextBar.Open
	order.Status = "filled"

	return order
}
