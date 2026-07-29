package algorithm

import (
	"time"
)

type Indicator struct {
	VWAP              float64 `json:"vwap"`
	StandardDeviation float64 `json:"standard_deviation"`
	UpperBand         float64 `json:"upper_band"`
	LowerBand         float64 `json:"lower_band"`
	ZScore            float64 `json:"z_score"`
}

type VWAPState struct {
	TotalVolume      float64 `json:"total_volume"`
	TotalPriceVolume float64 `json:"total_price_volume"`
}

type Signal struct {
	Symbol         string    `json:"symbol"`
	Action         string    `json:"action"` // "buy" or "sell" or "hold"
	Price          float64   `json:"price"`
	VWAP           float64   `json:"vwap"`
	ZScore         float64   `json:"z_score"`
	WinProbability float64   `json:"win_probability"`
	CreatedAt      time.Time `json:"created_at"`
}
