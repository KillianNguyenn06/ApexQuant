package algorithm

import (
	"math"
	"time"

	"apexquant/internal/account"
	"apexquant/internal/marketdata"
)

// type TradeAction string

// const (
// 	Hold TradeAction = "HOLD"
// 	Buy  TradeAction = "BUY"
// 	Sell TradeAction = "SELL"
// )

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
	Symbol string  `json:"symbol"`
	Action string  `json:"action"` // "buy" or "sell" or "hold"
	Price  float64 `json:"price"`
	VWAP   float64 `json:"vwap"`
	//ZScore         float64   `json:"z_score"`
	WinProbability float64   `json:"win_probability"`
	CreatedAt      time.Time `json:"created_at"`
}

// =================================================
// For every incoming price tick/candle
// =================================================
func TypicalPrice(bar marketdata.BarTick) float64 {
	return (bar.High + bar.Low + bar.Close) / 3
}

// =================================================
// VWAP Calculation
// =================================================
func VWAP(bar marketdata.BarTick, vwapState *VWAPState) float64 {
	vwapState.TotalPriceVolume += TypicalPrice(bar) * bar.Volume
	vwapState.TotalVolume += bar.Volume
	if vwapState.TotalVolume == 0 {
		return 0
	}
	return vwapState.TotalPriceVolume / vwapState.TotalVolume
}

// =================================================
// Standard Deviation
// =================================================
func StandardDeviation(bar marketdata.BarTick, VWAPState *VWAPState, indicator *Indicator) (float64, float64, float64) {
	var numerator float64
	var denominator float64
	k := 2.0 // band multiplier (commonly 1,2 or 3)
	numerator += math.Pow(TypicalPrice(bar)-VWAP(bar, VWAPState), 2) * bar.Volume
	denominator += bar.Volume

	if denominator == 0 {
		return 0, 0, 0
	} else {
		indicator.StandardDeviation = math.Sqrt(numerator / denominator)
	}

	indicator.UpperBand = VWAP(bar, VWAPState) + (k * indicator.StandardDeviation)
	indicator.LowerBand = VWAP(bar, VWAPState) - (k * indicator.StandardDeviation)

	return indicator.StandardDeviation, indicator.UpperBand, indicator.LowerBand
}

func DecisionMaking(indicator *Indicator, position *account.Position) Signal {

	// Buy shares
	if position.Quantity == 0 && position.CurrentPrice <= indicator.LowerBand {

		position.EntryPrice = position.CurrentPrice
		position.TakeProfitPrice = indicator.VWAP + indicator.StandardDeviation
		position.StopLossPrice = indicator.LowerBand - indicator.StandardDeviation

		return Signal{
			Symbol:    position.Symbol,
			Action:    "Buy",
			Price:     position.EntryPrice,
			VWAP:      indicator.VWAP,
			CreatedAt: time.Now(),
		}
	}

	if position.Quantity > 0 && position.CurrentPrice <= position.StopLossPrice {
		// Hit Stop Loss
		return Signal{
			Symbol:    position.Symbol,
			Action:    "Sell",
			Price:     position.CurrentPrice,
			VWAP:      indicator.VWAP,
			CreatedAt: time.Now(),
		}
	}

	if position.Quantity > 0 && position.CurrentPrice >= position.TakeProfitPrice {
		// Hit TP
		return Signal{
			Symbol:    position.Symbol,
			Action:    "Sell",
			Price:     position.CurrentPrice,
			VWAP:      indicator.VWAP,
			CreatedAt: time.Now(),
		}
	}

	return Signal{Action: "Hold"}
}
