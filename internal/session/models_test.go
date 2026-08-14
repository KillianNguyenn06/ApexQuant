package session

import (
	"apexquant/internal/account"
	"apexquant/internal/algorithm"
	"apexquant/internal/marketdata"
	"apexquant/internal/simulation"
	"math"
	"testing"
)

func requireClose(t *testing.T, name string, got float64, want float64) {
	t.Helper()
	const tolerance = 0.000001

	if math.Abs(got-want) > tolerance {
		t.Fatalf("%s: got %.8f, want %.8f", name, got, want)
	}
}

func TestPortfolioAllocate(t *testing.T) {
	tickers := []string{
		" aapl",
		"msft",
	}

	weights := []float64{
		0.40,
		0.60,
	}

	var allocations []PortfolioAllocation

	err := PortfolioAllocate(tickers, weights, &allocations)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(allocations) != 2 {
		t.Fatalf("allocations count: got %d, want 2", len(allocations))
	}

	if allocations[0].Symbol != "AAPL" {
		t.Fatalf("first symbol: got %s, want AAPL", allocations[0].Symbol)
	}
	requireClose(t, "AAPL weight", allocations[0].Weight, 0.40)

	if allocations[1].Symbol != "MSFT" {
		t.Fatalf("second symbol: got %s, want AAPL", allocations[1].Symbol)
	}
	requireClose(t, "MSFT weight", allocations[1].Weight, 0.60)
}

func TestPorfolioAllocateRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		tickers []string
		weights []float64
	}{
		{
			name:    "no tickers",
			tickers: nil,
			weights: nil,
		},
		{
			name: "more than eight tickers",
			tickers: []string{
				"A", "B", "C",
				"D", "E", "F",
				"G", "H", "I",
			},
			weights: []float64{
				0.12, 0.11, 0.11,
				0.11, 0.11, 0.11,
				0.11, 0.11, 0.11,
			},
		},
		{
			name:    "ticker and weight count mismatch",
			tickers: []string{"AAPL", "MSFT"},
			weights: []float64{1.0},
		},
		{
			name:    "zero weight",
			tickers: []string{"AAPL", "MSFT"},
			weights: []float64{1.0, 0},
		},
		{
			name:    "weights do not total one",
			tickers: []string{"AAPL", "MSFT"},
			weights: []float64{0.40, 0.40},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var allocations []PortfolioAllocation

			err := PortfolioAllocate(test.tickers, test.weights, &allocations)

			if err == nil {
				t.Fatalf("expected an allocation error")
			}
		})
	}
}

func TestFillPendingBuyOrder(t *testing.T) {
	pendingOrder := account.Order{
		ID:       "buy-1",
		Symbol:   "AAPL",
		Action:   "Buy",
		Quantity: 100,
		Status:   "Submitted",
	}

	position := account.Position{
		Symbol:          "AAPL",
		EntryPrice:      100,
		CurrentPrice:    100,
		StopLossPrice:   95,
		TakeProfitPrice: 110,
	}

	acc := account.Account{
		Equity:      10_000,
		Cash:        10_000,
		BuyingPower: 10_000,
	}

	nextBar := marketdata.BarTick{
		Symbol: "AAPL",
		Open:   110,
	}

	filledOrder, filled := FillPendingOrder(pendingOrder, &position, &acc, nextBar, 0.50)

	if !filled {
		t.Fatalf("expected Buy order to fill")
	}

	// Allocation is $5,000, at $110 per share, the maximum quantity is 5000 / 110
	expectedQuantity := 5_000.0 / 110.0

	requireClose(t, "filled quantity", filledOrder.Quantity, expectedQuantity)
	requireClose(t, "position quantity", position.Quantity, expectedQuantity)
	requireClose(t, "entry price", position.EntryPrice, 110)

	// Original distances are preserved:
	// Stop distance =  5 and target distance = 10
	requireClose(t, "stop loss", position.StopLossPrice, 105)
	requireClose(t, "take profit", position.TakeProfitPrice, 120)
	requireClose(t, "cash", acc.Cash, 5_000)
	requireClose(t, "buying power", acc.BuyingPower, 5_000)
}

func TestFillPendingSellOrder(t *testing.T) {
	pendingOrder := account.Order{
		ID:       "sell-1",
		Symbol:   "AAPL",
		Action:   "Sell",
		Quantity: 20,
		Status:   "Submitted",
	}

	position := account.Position{
		Symbol:          "AAPL",
		Quantity:        20,
		EntryPrice:      100,
		CurrentPrice:    110,
		StopLossPrice:   95,
		TakeProfitPrice: 115,
	}

	acc := account.Account{
		Equity:      4_400,
		Cash:        2_000,
		BuyingPower: 2_000,
	}

	nextBar := marketdata.BarTick{
		Symbol: "AAPL",
		Open:   120,
	}

	filledOrder, filled := FillPendingOrder(pendingOrder, &position, &acc, nextBar, 1.0)

	if !filled {
		t.Fatal("expected Sell order to fill")
	}

	requireClose(t, "filled quantity", filledOrder.Quantity, 20)
	requireClose(t, "position quantity", position.Quantity, 0)
	requireClose(t, "cash", acc.Cash, 4_400)
	requireClose(t, "buying power", acc.BuyingPower, 4_400)
	requireClose(t, "reset entry price", position.EntryPrice, 0)
	requireClose(t, "reset stop loss", position.StopLossPrice, 0)
	requireClose(t, "reset take profit", position.TakeProfitPrice, 0)
}

func TestSessionSubmitsSellOrder(t *testing.T) {
	signal := algorithm.Signal{
		Symbol: "AAPL",
		Action: "Sell",
		Price:  110,
	}

	position := account.Position{
		Symbol:   "AAPL",
		Quantity: 20,
	}

	acc := account.Account{
		Equity:      10_000,
		Cash:        8_000,
		BuyingPower: 8_000,
	}

	order, submitted := Session(
		signal,
		simulation.MonteCarlo{},
		&position,
		simulation.MonteCarloResult{},
		&acc,
		&account.Order{},
		1.0,
	)

	if !submitted {
		t.Fatal("expected Sell order to be submitted")
	}

	if order.Status != "Submitted" {
		t.Fatalf("status: got %s, want Submitted", order.Status)
	}

	requireClose(t, "Sell quantity", order.Quantity, 20)
}
