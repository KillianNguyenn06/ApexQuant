package broker

import (
	"apexquant/internal/account"
	"apexquant/internal/algorithm"
	"time"
)

func SubmitOrder(signal algorithm.Signal, quantity float64) account.Order {
	switch signal.Action {
	case "Buy":
		return account.Order{
			Symbol:    signal.Symbol,
			Action:    signal.Action,
			Quantity:  quantity,
			CreatedAt: time.Now(),
		}
	case "Sell":
		return account.Order{
			Symbol:    signal.Symbol,
			Action:    signal.Action,
			Quantity:  quantity,
			CreatedAt: time.Now(),
		}
	case "Hold":
		return account.Order{}
	}

	return account.Order{}
}
