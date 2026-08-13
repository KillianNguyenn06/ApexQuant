package session

import (
	"time"

	"apexquant/internal/account"
	"apexquant/internal/algorithm"
	"apexquant/internal/marketdata"
)

type BacktestSnapshot struct {
	Timestamp time.Time
	Bar       marketdata.BarTick
	Indicator algorithm.Indicator
	Signal    algorithm.Signal
	Position  account.Position
	Account   account.Account
	Order     *account.Order
}
