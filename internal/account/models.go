package account

import (
	"time"
)

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
	Symbol       string  `json:"symbol"`
	Quantity     float64 `json:"quantity"`
	EntryPrice   float64 `json:"entry_price"`
	CurrentPrice float64 `json:"current_price"`
	StrikePrice  float64 `json:"strike_price"`
	StopLosPrice float64 `json:"stop_loss_price"`
}
