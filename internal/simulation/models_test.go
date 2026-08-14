package simulation

import (
	"apexquant/internal/marketdata"
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

func TestAnnualizedVolatility(t *testing.T) {
	bars := []marketdata.BarTick{
		{Close: 100},
		{Close: 100 * math.Exp(0.1)},
		{Close: 100},
		{Close: 100 * math.Exp(0.1)},
	}

	got, err := AnnualizedVolatility(bars, 252)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Log returns are: 0.1, -0.1, 0.1
	expectedDailyVariance := 0.013333333333333334
	expected := math.Sqrt(expectedDailyVariance) * math.Sqrt(252)

	requireClose(t, "annualized volatility", got, expected)
}

func TestAnnualizedVolatilityRequiresThreeBars(t *testing.T) {
	bars := []marketdata.BarTick{
		{Close: 100},
		{Close: 105},
	}

	_, err := AnnualizedVolatility(bars, 252)
	if err == nil {
		t.Fatalf("Expected an error for fewer than three bars")
	}
}

func TestAnnualizedVolatilityRejectsInvalidClose(t *testing.T) {
	bars := []marketdata.BarTick{
		{Close: 100},
		{Close: 0},
		{Close: 105},
	}

	_, err := AnnualizedVolatility(bars, 252)

	if err == nil {
		t.Fatal("expected an error for a zero close price")
	}
}
