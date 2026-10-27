package simulation

import (
	"apexquant/internal/account"
	"apexquant/internal/algorithm"
	"apexquant/internal/marketdata"
	"math"
	"math/rand"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func reversionHistory(b float64) []marketdata.BarTick {
	rng := rand.New(rand.NewSource(42))
	bars := []marketdata.BarTick{}
	price := 100.0
	for i := 0; i < 500; i++ {
		bars = append(bars, marketdata.BarTick{Symbol: "AAPL", Timestamp: time.Date(2020, 1, 1+i, 0, 0, 0, 0, time.UTC), Open: price, High: price, Low: price, Close: price, VWAP: price, Volume: 100})
		var ind algorithm.Indicator
		algorithm.RollingVWAP(bars[max(0, len(bars)-20):], &ind)
		price *= math.Exp(.0001 + b*math.Log(ind.VWAP/price) + .006*rng.NormFloat64())
	}
	return bars
}
func TestEstimateReversionKnownPull(t *testing.T) {
	fit, e := EstimateReversion(reversionHistory(.3), 20, 252)
	if e != nil {
		t.Fatal(e)
	}
	want := math.Ln2 / -math.Log(.7)
	if math.Abs(fit.HalfLifeDays-want) > want*.4 || fit.Samples != 252 {
		t.Fatalf("bad fit %+v want half-life %v", fit, want)
	}
	k := -math.Log1p(-fit.Strength)
	if math.Abs(fit.AnnualLogDrift/252*(1-math.Exp(-k))/k-fit.Intercept) > 1e-12 {
		t.Fatal("fitted drift does not match daily mean")
	}
}
func TestEstimateRejectsCatchupFlatAndInsufficientHistory(t *testing.T) {
	bars := reversionHistory(.3)
	for _, input := range [][]marketdata.BarTick{bars[:50], nil} {
		if _, e := EstimateReversion(input, 20, 252); e == nil {
			t.Fatal("accepted insufficient history")
		}
	}
	// VWAP catches up to a stationary stock, but the actual stock never recovers.
	for i := range bars {
		bars[i].Close = 100
		bars[i].VWAP = 100 + 20*math.Exp(-float64(i)/100)
	}
	if _, e := EstimateReversion(bars, 20, 252); e == nil {
		t.Fatal("mistook moving VWAP for price recovery")
	}
	for i := range bars {
		bars[i].VWAP = 100
	}
	if _, e := EstimateReversion(bars, 20, 252); e == nil {
		t.Fatal("accepted constant gap")
	}
	if _, e := EstimateReversion(reversionHistory(-.03), 20, 252); e == nil {
		t.Fatal("accepted divergence")
	}
}
func TestAutoModelExactOneDayConditionalMean(t *testing.T) {
	input := MonteCarlo{Model: "vwap_reversion_auto", AutoLogDrift: .02, ReversionHalfLifeDays: 5, HoldingDays: 1, TimeHorizon: 1.0 / 252, NumPaths: 1, NumSteps: 1, Seed: 42}
	p := account.Position{EntryPrice: 90, StopLossPrice: 1, TakeProfitPrice: 200}
	signal := algorithm.Signal{Price: 90, VWAP: 100}
	var r MonteCarloResult
	GBMModel(input, p, &r, signal)
	k := 252 * math.Ln2 / 5
	decay := math.Exp(-k / 252)
	want := math.Exp(math.Log(100) + (math.Log(90)-math.Log(100))*decay + .02*(1-decay)/k)
	if math.Abs(r.TimeoutMeanReturn-(want/90-1)) > 1e-12 {
		t.Fatal("auto model did not use fitted drift and half-life")
	}
}

func BenchmarkReversionSimulation(b *testing.B) {
	input := MonteCarlo{Model: "vwap_reversion", ReversionHalfLifeDays: 5, Volatility: .2, RiskFreeRate: .03, HoldingDays: 5, TimeHorizon: 5.0 / 252, NumPaths: 10000, NumSteps: 252, Seed: 42}
	p := account.Position{EntryPrice: 95, StopLossPrice: 90, TakeProfitPrice: 105}
	signal := algorithm.Signal{Price: 95, VWAP: 100}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var r MonteCarloResult
		GBMModel(input, p, &r, signal)
	}
}

func TestSimulationModelsConserveOutcomesAcrossWorkerCounts(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	for _, model := range []string{"gbm", "vwap_reversion", "vwap_reversion_auto"} {
		input := MonteCarlo{Model: model, AutoLogDrift: .02, ReversionHalfLifeDays: 5, Volatility: .2, RiskFreeRate: .03, HoldingDays: 5, TimeHorizon: 5.0 / 252, NumPaths: 1000, NumSteps: 252, Seed: 42}
		p := account.Position{EntryPrice: 95, StopLossPrice: 90, TakeProfitPrice: 105}
		signal := algorithm.Signal{Price: 95, VWAP: 100}
		var first, second MonteCarloResult
		runtime.GOMAXPROCS(1)
		GBMModel(input, p, &first, signal)
		runtime.GOMAXPROCS(4)
		GBMModel(input, p, &second, signal)
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("%s changed with worker count", model)
		}
		if math.Abs(first.WinProbability+first.LossProbability+first.TimeoutProbability-1) > 1e-12 || len(first.Payoffs) != input.NumPaths {
			t.Fatalf("%s lost outcomes", model)
		}
		for _, r := range first.Payoffs {
			if math.IsNaN(r) || math.IsInf(r, 0) {
				t.Fatalf("%s invalid payoff", model)
			}
		}
	}
}
