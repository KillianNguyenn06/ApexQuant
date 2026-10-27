package backtest

import (
	"apexquant/internal/algorithm"
	"apexquant/internal/marketdata"
	"context"
	"math"
	"math/rand"
	"testing"
	"time"
)

func TestAutoReversionFrozenBeforeRunAndAccounting(t *testing.T) {
	c := dailyTouchConfig()
	c.MonteCarloInput.Model = "vwap_reversion_auto"
	c.DailyVWAPWindow = 20
	rng := rand.New(rand.NewSource(42))
	history := []marketdata.BarTick{}
	price := 100.0
	first := c.BarsBySymbol["AAPL"][0].Timestamp
	for i := 0; i < 273; i++ {
		b := marketdata.BarTick{Symbol: "AAPL", Timestamp: first.AddDate(0, 0, i-274), Open: price, High: price, Low: price, Close: price, VWAP: price, Volume: 100}
		history = append(history, b)
		var ind algorithm.Indicator
		algorithm.RollingVWAP(history[max(0, len(history)-20):], &ind)
		price *= math.Exp(.3*math.Log(ind.VWAP/price) + .006*rng.NormFloat64())
	}
	c.VolatilityHistory = map[string][]marketdata.BarTick{"AAPL": history}
	if e := prepareReversion(&c); e != nil {
		t.Fatal(e)
	}
	fit := c.ReversionEstimates["AAPL"]
	r, e := RunBacktest(c)
	if e != nil {
		t.Fatal(e)
	}
	reconcile(t, c, r)
	c.BarsBySymbol["AAPL"][4].Close *= 1.01
	if e := prepareReversion(&c); e != nil {
		t.Fatal(e)
	}
	if fit != c.ReversionEstimates["AAPL"] {
		t.Fatal("future trading bar affected calibration")
	}
	c.VolatilityHistory["AAPL"][272].Timestamp = first.Add(time.Hour)
	if e := prepareReversion(&c); e == nil {
		t.Fatal("overlapping history accepted")
	}
}
func TestAutoRejectsMissingHistoryWithoutFallback(t *testing.T) {
	c := dailyTouchConfig()
	c.MonteCarloInput.Model = "vwap_reversion_auto"
	if _, e := RunBacktest(c); e == nil {
		t.Fatal("silently fell back without calibration")
	}
	c.MonteCarloInput.Model = "vwap_reversion"
	c.MonteCarloInput.ReversionHalfLifeDays = 5
	if _, e := RunBacktest(c); e != nil {
		t.Fatal("manual override required auto history", e)
	}
}

type autoHistoryProvider struct{ bars []marketdata.BarTick }

func (p autoHistoryProvider) FetchBars(_ context.Context, symbol string, start, end time.Time, _, _ string) ([]marketdata.BarTick, error) {
	out := []marketdata.BarTick{}
	for _, b := range p.bars {
		if !b.Timestamp.Before(start) && b.Timestamp.Before(end) {
			b.Symbol = symbol
			out = append(out, b)
		}
	}
	return out, nil
}
func (p autoHistoryProvider) FetchRiskFreeRate(context.Context, string, time.Time) (float64, error) {
	return .03, nil
}
func TestServiceAutoFitsAndPublishesPreRunEstimate(t *testing.T) {
	c := dailyTouchConfig()
	rng := rand.New(rand.NewSource(42))
	history := []marketdata.BarTick{}
	price := 100.0
	for i := 0; i < 500; i++ {
		b := marketdata.BarTick{Symbol: "AAPL", Timestamp: time.Date(2020, 1, 1+i, 5, 0, 0, 0, time.UTC), Open: price, High: price, Low: price, Close: price, VWAP: price, Volume: 100}
		history = append(history, b)
		var ind algorithm.Indicator
		algorithm.RollingVWAP(history[max(0, len(history)-20):], &ind)
		price *= math.Exp(.3*math.Log(ind.VWAP/price) + .006*rng.NormFloat64())
	}
	settings := DefaultServiceSettings()
	settings.MonteCarlo.NumPaths = 100
	settings.MonteCarlo.Seed = 42
	service, e := NewService(autoHistoryProvider{history}, Credentials{}, settings)
	if e != nil {
		t.Fatal(e)
	}
	request := RunRequest{ProbabilityModel: "vwap_reversion_auto", DailyVWAPWindow: 20, InitialCapital: 10000, Allocations: c.Allocations, Start: history[400].Timestamp, End: history[410].Timestamp}
	out, e := service.Run(context.Background(), request)
	if e != nil {
		t.Fatal(e)
	}
	fit := out.Config.ReversionEstimates["AAPL"]
	if fit.Samples != 252 || fit.HalfLifeDays <= 0 || fit.Through >= request.Start.Format(time.DateOnly) {
		t.Fatalf("bad calibration metadata %+v", fit)
	}
	reconcile(t, out.Config, out.Result)
}
