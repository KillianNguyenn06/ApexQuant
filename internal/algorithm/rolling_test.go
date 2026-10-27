package algorithm

import (
	"apexquant/internal/marketdata"
	"math"
	"testing"
)

func TestRollingVWAPWeightedMeanAndBandsShareWindow(t *testing.T) {
	bars := []marketdata.BarTick{{VWAP: 10, Volume: 3}, {VWAP: 20, Volume: 1}, {VWAP: 9999, Volume: 0}}
	var got Indicator
	RollingVWAP(bars, &got)
	sd := math.Sqrt(18.75)
	if got.VWAP != 12.5 || math.Abs(got.StandardDeviation-sd) > 1e-12 || got.LowerBand != 12.5-sd || got.UpperBand != 12.5+sd || !got.VolumeReady {
		t.Fatalf("wrong weighted window: %+v", got)
	}
	RollingVWAP(bars[2:], &got)
	if got.VolumeReady || got.VWAP != 0 || got.StandardDeviation != 0 || got.UpperBand != 0 || got.LowerBand != 0 {
		t.Fatalf("expired volume leaked: %+v", got)
	}
}

func TestRollingVWAPKeepsSmallVarianceAtLargePrices(t *testing.T) {
	var got Indicator
	RollingVWAP([]marketdata.BarTick{{VWAP: 1e10, Volume: 1}, {VWAP: 1e10 + 2, Volume: 1}}, &got)
	if got.StandardDeviation != 1 {
		t.Fatalf("variance canceled: %+v", got)
	}
}
