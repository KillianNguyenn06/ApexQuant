package algorithm

import (
	"math"
	"time"

	"apexquant/internal/account"
	"apexquant/internal/marketdata"
)

type Indicator struct {
	VWAP              float64 `json:"vwap"`
	StandardDeviation float64 `json:"standard_deviation"`
	UpperBand         float64 `json:"upper_band"`
	LowerBand         float64 `json:"lower_band"`
	ZScore            float64 `json:"z_score"`
}

type VWAPState struct {
	TotalVolume             float64 `json:"total_volume"`
	TotalPriceVolume        float64 `json:"total_price_volume"`
	TotalSquaredPriceVolume float64 `json:"total_squared_price_volume"`
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
	return (bar.High + bar.Low + bar.Close) / 3 // Since we fetch bar.VWAP, we dont need this. Leave it as a fallback
}

// =================================================
// VWAP Calculation
// =================================================
func VWAP(bar marketdata.BarTick, vwapState *VWAPState, indicator *Indicator) float64 {
	vwapState.TotalPriceVolume += bar.VWAP * bar.Volume
	vwapState.TotalVolume += bar.Volume
	if vwapState.TotalVolume == 0 {
		return 0
	}
	indicator.VWAP = vwapState.TotalPriceVolume / vwapState.TotalVolume
	return indicator.VWAP
}

// =================================================
// Standard Deviation
// =================================================
func StandardDeviation(bar marketdata.BarTick, VWAPState *VWAPState, indicator *Indicator) (float64, float64, float64) {

	k := 1.0                                                                // band multiplier (commonly 1,2 or 3)
	VWAPState.TotalSquaredPriceVolume += math.Pow(bar.VWAP, 2) * bar.Volume // Basically the numerator
	variance := VWAPState.TotalSquaredPriceVolume/VWAPState.TotalVolume - math.Pow(indicator.VWAP, 2)
	variance = math.Max(0, variance)
	indicator.StandardDeviation = math.Sqrt(variance)

	indicator.UpperBand = indicator.VWAP + (k * indicator.StandardDeviation)
	indicator.LowerBand = indicator.VWAP - (k * indicator.StandardDeviation)

	return indicator.StandardDeviation, indicator.UpperBand, indicator.LowerBand
}

// =================================================
// Decision Making (send signals)
// =================================================
func DecisionMaking(indicator *Indicator, position *account.Position, bar marketdata.BarTick) Signal {

	// Buy shares
	if position.Quantity == 0 && indicator.StandardDeviation > 0 && position.CurrentPrice <= indicator.LowerBand {

		position.EntryPrice = position.CurrentPrice
		position.TakeProfitPrice = indicator.VWAP + 0.5*indicator.StandardDeviation
		position.StopLossPrice = position.EntryPrice - indicator.StandardDeviation

		return Signal{
			Symbol:    position.Symbol,
			Action:    "Buy",
			Price:     position.EntryPrice,
			VWAP:      indicator.VWAP,
			CreatedAt: bar.Timestamp,
		}
	}

	if position.Quantity > 0 && position.CurrentPrice <= position.StopLossPrice {
		// Hit Stop Loss
		return Signal{
			Symbol:    position.Symbol,
			Action:    "Sell",
			Price:     position.CurrentPrice,
			VWAP:      indicator.VWAP,
			CreatedAt: bar.Timestamp,
		}
	}

	if position.Quantity > 0 && position.CurrentPrice >= position.TakeProfitPrice {
		// Hit TP
		return Signal{
			Symbol:    position.Symbol,
			Action:    "Sell",
			Price:     position.CurrentPrice,
			VWAP:      indicator.VWAP,
			CreatedAt: bar.Timestamp,
		}
	}

	return Signal{Action: "Hold"}
}
