package algorithm

import (
	"apexquant/internal/marketdata"
	"math"
)

// RollingVWAP uses the same observed-bar window for its mean and bands.
// Variance is centered to avoid cancellation when prices are large.
func RollingVWAP(bars []marketdata.BarTick, indicator *Indicator) {
	var volume, priceVolume float64
	for _, bar := range bars {
		volume += bar.Volume
		priceVolume += bar.VWAP * bar.Volume
	}
	indicator.VolumeReady = volume > 0
	indicator.VWAP, indicator.StandardDeviation, indicator.UpperBand, indicator.LowerBand = 0, 0, 0, 0
	if volume <= 0 {
		return
	}
	indicator.VWAP = priceVolume / volume
	var variance float64
	for _, bar := range bars {
		difference := bar.VWAP - indicator.VWAP
		variance += bar.Volume * difference * difference
	}
	indicator.StandardDeviation = math.Sqrt(variance / volume)
	indicator.UpperBand = indicator.VWAP + indicator.StandardDeviation
	indicator.LowerBand = indicator.VWAP - indicator.StandardDeviation
}
