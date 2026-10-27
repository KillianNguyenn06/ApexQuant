package simulation

import (
	"apexquant/internal/algorithm"
	"apexquant/internal/marketdata"
	"fmt"
	"math"
)

// ReversionEstimate is fitted once using only pre-run observations, then frozen.
// Regression: log(P[t+1]/P[t]) = intercept + strength*log(V[t]/P[t]) + error.
// Using price returns, rather than changes in P/V, excludes VWAP catch-up.
type ReversionEstimate struct {
	HalfLifeDays   float64 `json:"half_life_days"`
	Strength       float64 `json:"strength"`
	StandardError  float64 `json:"standard_error"`
	Intercept      float64 `json:"daily_log_intercept"`
	AnnualLogDrift float64 `json:"annual_log_drift"`
	Samples        int     `json:"samples"`
	Through        string  `json:"through"`
}

func EstimateReversion(bars []marketdata.BarTick, window int, periods float64) (ReversionEstimate, error) {
	fail := func() (ReversionEstimate, error) {
		return ReversionEstimate{}, fmt.Errorf("unreliable reversion estimate")
	}
	if window < 2 || window > 252 || periods <= 0 || math.IsNaN(periods) || math.IsInf(periods, 0) {
		return fail()
	}
	x, y := []float64{}, []float64{}
	for i := max(window-1, len(bars)-253); i+1 < len(bars); i++ {
		if bars[i].Close <= 0 || bars[i+1].Close <= 0 || bars[i].Volume <= 0 || bars[i+1].Volume <= 0 {
			return fail()
		}
		if i > 0 && !bars[i].Timestamp.After(bars[i-1].Timestamp) {
			return fail()
		}
		if !bars[i+1].Timestamp.After(bars[i].Timestamp) {
			return fail()
		}
		var indicator algorithm.Indicator
		algorithm.RollingVWAP(bars[i+1-window:i+1], &indicator)
		if !indicator.VolumeReady || indicator.VWAP <= 0 {
			return fail()
		}
		gap := math.Log(indicator.VWAP / bars[i].Close)
		ret := math.Log(bars[i+1].Close / bars[i].Close)
		if math.IsNaN(gap+ret) || math.IsInf(gap+ret, 0) {
			return fail()
		}
		x = append(x, gap)
		y = append(y, ret)
	}

	// Linear Regression for Auto Mean Reversion
	n := len(x)
	if n < 60 {
		return fail()
	}
	mx, my := 0.0, 0.0
	for i := range x {
		mx += x[i] / float64(n)
		my += y[i] / float64(n)
	}
	xx, xy := 0.0, 0.0
	for i := range x {
		xx += (x[i] - mx) * (x[i] - mx)
		xy += (x[i] - mx) * (y[i] - my)
	}
	if xx < 1e-12 {
		return fail()
	}
	b := xy / xx
	c := my - b*mx
	// Newey-West (5 lags) slope uncertainty tolerates short serial dependence.
	score := make([]float64, n)
	meat := 0.0
	for i := range x {
		score[i] = (x[i] - mx) * (y[i] - c - b*x[i])
		meat += score[i] * score[i]
	}
	for lag := 1; lag <= 5; lag++ {
		cross := 0.0
		for i := lag; i < n; i++ {
			cross += score[i] * score[i-lag]
		}
		meat += 2 * (1 - float64(lag)/6) * cross
	}
	se := math.Sqrt(max(0, meat)*float64(n)/float64(n-2)) / xx
	if math.IsNaN(b+se+c) || math.IsInf(b+se+c, 0) || b-1.96*se <= 0 || b+1.96*se >= 1 {
		return fail()
	}
	k := -math.Log1p(-b)
	half := math.Ln2 / k
	if half < 0.1 || half > 252 {
		return fail()
	}
	return ReversionEstimate{HalfLifeDays: half, Strength: b, StandardError: se, Intercept: c, AnnualLogDrift: c * k / b * periods, Samples: n, Through: bars[len(bars)-1].Timestamp.Format("2006-01-02")}, nil
}
