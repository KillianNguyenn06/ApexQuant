package risk

import (
	"apexquant/internal/simulation"
	"testing"
)

func TestKellyUsesAllTerminalPayoffs(t *testing.T) {
	result := simulation.MonteCarloResult{Payoffs: []float64{2, 2, -1, -1, -1, 0, 0, 0, 0, 0}}
	requireQuantity(t, "independent expected-log optimum", KellyCriterion(result, basePosition()), .1)
	// Same barrier counts, but timeout losses change the edge's sign.
	result.Payoffs = []float64{2, 2, -1, -1, -1, -.5, -.5, -.5, -.5, -.5}
	requireQuantity(t, "losing timeouts reject", KellyCriterion(result, basePosition()), 0)
	result.Payoffs = []float64{.2, .2, .2}
	requireQuantity(t, "positive timeouts contribute", KellyCriterion(result, basePosition()), 1)
	result = simulation.MonteCarloResult{WinProbability: .2, LossProbability: .3, TimeoutProbability: .5}
	requireQuantity(t, "zero-payoff timeout analytical optimum", KellyCriterion(result, basePosition()), .1)
}
