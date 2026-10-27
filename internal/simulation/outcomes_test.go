package simulation

import (
	"apexquant/internal/account"
	"apexquant/internal/algorithm"
	"math"
	"reflect"
	"testing"
)

func TestTerminalPayoffsAndReversion(t *testing.T) {
	p := account.Position{EntryPrice: 100, StopLossPrice: 50, TakeProfitPrice: 200}
	signal := algorithm.Signal{Price: 100, VWAP: 121}
	input := MonteCarlo{HoldingDays: 1, TimeHorizon: 1.0 / 252, NumPaths: 8, NumSteps: 10, Seed: 42, Model: "vwap_reversion", ReversionHalfLifeDays: 1}
	var got MonteCarloResult
	GBMModel(input, p, &got, signal)
	// With no noise/drift, one half-life halves log distance: sqrt(100*121)=110.
	requireClose(t, "timeout probability", got.TimeoutProbability, 1)
	requireClose(t, "timeout terminal return", got.TimeoutMeanReturn, .1)
	for _, r := range got.Payoffs {
		requireClose(t, "terminal risk payoff", r, .2)
	}
	input.Model = "gbm"
	input.RiskFreeRate = math.Log(1.02) * 252
	GBMModel(input, p, &got, signal)
	requireClose(t, "GBM terminal return", got.TimeoutMeanReturn, .02)
	input.RiskFreeRate = 0
	input.Volatility = .3
	var again MonteCarloResult
	GBMModel(input, p, &got, signal)
	GBMModel(input, p, &again, signal)
	if !reflect.DeepEqual(got, again) {
		t.Fatal("seeded path results not deterministic")
	}
	requireClose(t, "probability mass", got.WinProbability+got.LossProbability+got.TimeoutProbability, 1)
}
func TestBarrierPayoffsRetained(t *testing.T) {
	p := account.Position{EntryPrice: 100, StopLossPrice: 95, TakeProfitPrice: 110}
	input := MonteCarlo{HoldingDays: 5, TimeHorizon: 5.0 / 252, NumPaths: 8, NumSteps: 20, Seed: 42, RiskFreeRate: 100}
	var got MonteCarloResult
	GBMModel(input, p, &got, algorithm.Signal{Price: 100})
	requireClose(t, "win", got.WinProbability, 1)
	for _, r := range got.Payoffs {
		requireClose(t, "target payoff", r, 2)
	}
	input.RiskFreeRate = -100
	GBMModel(input, p, &got, algorithm.Signal{Price: 100})
	requireClose(t, "loss", got.LossProbability, 1)
	for _, r := range got.Payoffs {
		requireClose(t, "stop payoff", r, -1)
	}
}
