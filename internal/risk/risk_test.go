package risk

import (
	"math"
	"testing"

	"apexquant/internal/account"
	"apexquant/internal/simulation"
)

func requireQuantity(t *testing.T, name string, got float64, want float64) {
	t.Helper()
	const tolerance = 0.000001

	if math.Abs(got-want) > tolerance {
		t.Fatalf("%s: got %.8f, want %.8f", name, got, want)
	}
}

func basePosition() account.Position {
	return account.Position{
		Symbol:          "AAPL",
		EntryPrice:      100,
		CurrentPrice:    100,
		StopLossPrice:   95,
		TakeProfitPrice: 110,
	}
}

func TestKellyCriterion(t *testing.T) {
	result := simulation.MonteCarloResult{
		WinProbability:  0.60,
		LossProbability: 0.40,
	}

	got := KellyCriterion(result, basePosition())

	// Reward/Risk ratio = 10/5 = 2
	// Kelly = ((2 * 0.60) - 0.40) / 2 = 0.40
	requireQuantity(t, "Kelly fraction", got, 0.40)
}

func TestKellyCriterionRejectsInvalidStopLoss(t *testing.T) {
	position := basePosition()
	position.StopLossPrice = position.EntryPrice

	result := simulation.MonteCarloResult{
		WinProbability:  0.60,
		LossProbability: 0.40,
	}

	got := KellyCriterion(result, position)

	requireQuantity(t, "invalid stop-loss kelly fraction", got, 0)
}

func TestEvaluateRiskLimits(t *testing.T) {
	strongResult := simulation.MonteCarloResult{
		WinProbability:  0.60,
		LossProbability: 0.40,
	}

	tests := []struct {
		name             string
		result           simulation.MonteCarloResult
		position         account.Position
		account          account.Account
		allocationWeight float64
		wantQuantity     float64
	}{
		{
			name:     "limited by symbol allocation",
			result:   strongResult,
			position: basePosition(),
			account: account.Account{
				Equity:      10_000,
				Cash:        10_000,
				BuyingPower: 10_000,
			},
			allocationWeight: 0.50,
			wantQuantity:     50,
		},
		{
			name:     "limtied by cash",
			result:   strongResult,
			position: basePosition(),
			account: account.Account{
				Equity:      10_000,
				Cash:        2_000,
				BuyingPower: 10_000,
			},
			allocationWeight: 1.0,
			wantQuantity:     20,
		},
		{
			name:     "limited by buying power",
			result:   strongResult,
			position: basePosition(),
			account: account.Account{
				Equity:      10_000,
				Cash:        10_000,
				BuyingPower: 1_500,
			},
			allocationWeight: 1.0,
			wantQuantity:     15,
		},
		{
			name: "limited by Kelly risk",
			result: simulation.MonteCarloResult{
				WinProbability:  0.45,
				LossProbability: 0.55,
			},
			position: basePosition(),
			account: account.Account{
				Equity:      10_000,
				Cash:        10_000,
				BuyingPower: 10_000,
			},
			allocationWeight: 1.0,
			wantQuantity:     70,
		},
		{
			name:   "limited by remaining allocation",
			result: strongResult,
			position: account.Position{
				Symbol:          "AAPL",
				Quantity:        40,
				EntryPrice:      100,
				CurrentPrice:    100,
				StopLossPrice:   95,
				TakeProfitPrice: 110,
			},
			account: account.Account{
				Equity:      10_000,
				Cash:        10_000,
				BuyingPower: 10_000,
			},
			allocationWeight: 0.50,
			wantQuantity:     10,
		},
		{
			name: "rejects negative Kelly edge",
			result: simulation.MonteCarloResult{
				WinProbability:  0.10,
				LossProbability: 0.90,
			},
			position: basePosition(),
			account: account.Account{
				Equity:      10_000,
				Cash:        10_000,
				BuyingPower: 10_000,
			},
			allocationWeight: 1.0,
			wantQuantity:     0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := EvaluateRisk(test.result, test.position, test.account, test.allocationWeight)
			requireQuantity(t, test.name, got, test.wantQuantity)
		})
	}
}
