package backtest

import (
	"math"
	"testing"
	"time"

	"apexquant/internal/account"
	"apexquant/internal/marketdata"
	"apexquant/internal/session"
	"apexquant/internal/simulation"
)

func TestRunBacktestMultiSymbol(t *testing.T) {
	aaplBars := []marketdata.BarTick{
		testBar("AAPL", 0, 100, 100, 100),
		testBar("AAPL", 1, 100, 90, 110),
		testBar("AAPL", 2, 91, 110, 110),
		testBar("AAPL", 3, 109, 109, 109),
	}

	msftBars := []marketdata.BarTick{
		testBar("MSFT", 0, 200, 200, 200),
		testBar("MSFT", 1, 201, 201, 201),
		testBar("MSFT", 2, 202, 202, 202),
		testBar("MSFT", 3, 203, 203, 203),
	}

	config := BacktestConfig{
		InitialAccount: account.Account{
			Equity:      10_000,
			Cash:        10_000,
			BuyingPower: 10_000,
		},
		Allocations: []session.PortfolioAllocation{
			{Symbol: "AAPL", Weight: 0.50},
			{Symbol: "MSFT", Weight: 0.50},
		},
		BarsBySymbol: map[string][]marketdata.BarTick{
			"AAPL": aaplBars,
			"MSFT": msftBars,
		},
		MonteCarloInput: simulation.MonteCarlo{
			Volatility:   0.05,
			RiskFreeRate: 1.00,
			TimeHorizon:  1.00,
			NumPaths:     100,
			NumSteps:     10,
			Seed:         42,
		},
		VolatilityWindow: 3,
		PeriodsPerYear:   252,
	}

	result, err := RunBacktest(config)
	if err != nil {
		t.Fatalf("RunBacktest returned an error: %v", err)
	}

	if got, want := len(result.Snapshots), 8; got != want {
		t.Fatalf("snapshot count = %d, want %d", got, want)
	}

	buySubmission := result.Snapshots[2]

	if buySubmission.Signal.Action != "Buy" {
		t.Fatalf(
			"AAPL signal = %s, want Buy",
			buySubmission.Signal.Action,
		)
	}

	if buySubmission.SubmittedOrder == nil {
		t.Fatal("expected an AAPL Buy order to be submitted")
	}

	buyFillAndSellSubmission := result.Snapshots[4]

	if buyFillAndSellSubmission.FilledOrder == nil {
		t.Fatal("expected the AAPL Buy order to fill")
	}

	if buyFillAndSellSubmission.FilledOrder.Action != "Buy" {
		t.Fatalf(
			"filled action = %s, want Buy",
			buyFillAndSellSubmission.FilledOrder.Action,
		)
	}

	if buyFillAndSellSubmission.Position.Quantity <= 0 {
		t.Fatal("expected AAPL position quantity to be positive")
	}

	if buyFillAndSellSubmission.SubmittedOrder == nil {
		t.Fatal("expected an AAPL Sell order to be submitted")
	}

	if buyFillAndSellSubmission.SubmittedOrder.Action != "Sell" {
		t.Fatalf(
			"submitted action = %s, want Sell",
			buyFillAndSellSubmission.SubmittedOrder.Action,
		)
	}

	sellFill := result.Snapshots[6]

	if sellFill.FilledOrder == nil {
		t.Fatal("expected the AAPL Sell order to fill")
	}

	if sellFill.FilledOrder.Action != "Sell" {
		t.Fatalf(
			"filled action = %s, want Sell",
			sellFill.FilledOrder.Action,
		)
	}

	if result.FinalPositions["AAPL"].Quantity != 0 {
		t.Fatalf(
			"final AAPL quantity = %.2f, want 0",
			result.FinalPositions["AAPL"].Quantity,
		)
	}

	if result.FinalPositions["MSFT"].Quantity != 0 {
		t.Fatalf(
			"final MSFT quantity = %.2f, want 0",
			result.FinalPositions["MSFT"].Quantity,
		)
	}

	for i := 1; i < len(result.Snapshots); i += 2 {
		snapshot := result.Snapshots[i]

		if snapshot.Bar.Symbol != "MSFT" {
			t.Fatalf(
				"snapshot %d symbol = %s, want MSFT",
				i,
				snapshot.Bar.Symbol,
			)
		}

		if snapshot.SubmittedOrder != nil ||
			snapshot.FilledOrder != nil {
			t.Fatalf(
				"MSFT unexpectedly traded at snapshot %d",
				i,
			)
		}
	}

	if result.FinalAccount.Cash <= config.InitialAccount.Cash {
		t.Fatalf(
			"final cash = %.2f, want more than %.2f",
			result.FinalAccount.Cash,
			config.InitialAccount.Cash,
		)
	}

	if math.Abs(
		result.FinalAccount.Equity-result.FinalAccount.Cash,
	) > 0.000001 {
		t.Fatalf(
			"final equity %.2f does not equal final cash %.2f",
			result.FinalAccount.Equity,
			result.FinalAccount.Cash,
		)
	}
}

func testBar(
	symbol string,
	day int,
	open float64,
	close float64,
	vwap float64,
) marketdata.BarTick {
	return marketdata.BarTick{
		Symbol: symbol,
		Open:   open,
		High:   math.Max(open, close),
		Low:    math.Min(open, close),
		Close:  close,
		Volume: 1_000,
		VWAP:   vwap,
		Timestamp: time.Date(
			2025,
			time.January,
			2+day,
			0,
			0,
			0,
			0,
			time.UTC,
		),
	}
}
