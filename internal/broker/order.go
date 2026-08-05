package broker

import (
	"apexquant/internal/account"
	"apexquant/internal/algorithm"
	"apexquant/internal/marketdata"
	"time"

	"github.com/google/uuid"
)

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
			CreatedAt: time.Now(),
		}
	case "Sell":
		return account.Order{
			ID:        idV4.String(),
			Symbol:    signal.Symbol,
			Action:    signal.Action,
			Quantity:  quantity,
			Status:    "Submitted",
			CreatedAt: time.Now(),
		}
	case "Hold":
		return account.Order{}
	}

	return account.Order{}
}

func FillOrderAtNextBar(order account.Order, nextBar marketdata.BarTick) account.Order {
	if order.Symbol != nextBar.Symbol {
		return order
	}
	order.FilledPrice = nextBar.Open
	order.Status = "filled"

	return order
}
