package account

import (
	"time"
)

type Account struct {
	Equity      float64
	Cash        float64
	BuyingPower float64
}

type Order struct {
	ID          string    `json:"id"`
	Symbol      string    `json:"symbol"`
	Action      string    `json:"action"`
	Quantity    float64   `json:"quantity"`
	FilledPrice float64   `json:"filled_price"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

type Position struct {
	Symbol          string  `json:"symbol"`
	Quantity        float64 `json:"quantity"`
	EntryPrice      float64 `json:"entry_price"`
	CurrentPrice    float64 `json:"current_price"`
	TakeProfitPrice float64 `json:"take_profit_price"`
	StopLossPrice   float64 `json:"stop_loss_price"`
}

type Logging struct {
	Symbol        string    `json:"symbol"`
	OpenPosition  time.Time `json:"position_open"`
	ClosePosition time.Time `json:"position_close"`
	Entry         float64   `json:"entry"`
	Exit          float64   `json:"exit"`
	PnL           float64   `json:"pnl"`
}
