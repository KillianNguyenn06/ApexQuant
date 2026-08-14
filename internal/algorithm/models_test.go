package algorithm

import (
	"math"
	"testing"
	"time"

	"apexquant/internal/account"
	"apexquant/internal/marketdata"
)

func requireClose(t *testing.T, name string, got float64, want float64) {
	t.Helper()
	const tolerance = 0.000001

	if math.Abs(got-want) > tolerance {
		t.Fatalf("%s: got %.8f, want %.8f", name, got, want)
	}
}

func TestVWAPAndStandardDeviation(t *testing.T) {
	state := VWAPState{}
	indicator := Indicator{}

	firstBar := marketdata.BarTick{
		VWAP:   100,
		Volume: 10,
	}

	VWAP(firstBar, &state, &indicator)
	StandardDeviation(firstBar, &state, &indicator)

	requireClose(t, "first VWAP", indicator.VWAP, 100)
	requireClose(t, "first standard deviation", indicator.StandardDeviation, 0)

	secondBar := marketdata.BarTick{
		VWAP:   110,
		Volume: 30,
	}

	VWAP(secondBar, &state, &indicator)
	StandardDeviation(secondBar, &state, &indicator)

	expectedVWAP := 107.5
	expectedStandardDeviation := math.Sqrt(18.75) // 18.75 is the variance's value

	requireClose(t, "second VWAP", indicator.VWAP, expectedVWAP)
	requireClose(t, "standard deviation", indicator.StandardDeviation, expectedStandardDeviation)

	requireClose(t, "upper band", indicator.UpperBand, expectedVWAP+expectedStandardDeviation)
	requireClose(t, "lower band", indicator.LowerBand, expectedVWAP-expectedStandardDeviation)

}

func TestVWAPWithZeroVolume(t *testing.T) {
	state := VWAPState{}
	indicator := Indicator{}

	bar := marketdata.BarTick{
		VWAP:   100,
		Volume: 0,
	}

	result := VWAP(bar, &state, &indicator)

	requireClose(t, "VWAP Result", result, 0)
	requireClose(t, "indicator VWAP", indicator.VWAP, 0)
	requireClose(t, "total volume", state.TotalVolume, 0)
}

func TestDecisionMakingBuy(t *testing.T) {
	timestamp := time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC)

	indicator := Indicator{
		VWAP:              100,
		StandardDeviation: 2,
		LowerBand:         98,
		UpperBand:         102,
	}

	position := account.Position{
		Symbol:       "AAPL",
		Quantity:     0,
		CurrentPrice: 97,
	}

	bar := marketdata.BarTick{
		Symbol:    "AAPL",
		Timestamp: timestamp,
	}

	signal := DecisionMaking(&indicator, &position, bar)

	if signal.Action != "Buy" {
		t.Fatalf("action: got %s, want Buy", signal.Action)
	}

	requireClose(t, "signal price", signal.Price, 97)
	requireClose(t, "entry price", position.EntryPrice, 97)
	requireClose(t, "stop loss", position.StopLossPrice, 95)
	requireClose(t, "take profit", position.TakeProfitPrice, 101)

	if signal.Symbol != "AAPL" {
		t.Fatalf("Symbol: got %s, want AAPL", signal.Symbol)
	}

	if !signal.CreatedAt.Equal(timestamp) {
		t.Fatalf("timestamp: got %v, want %v", signal.CreatedAt, timestamp)
	}
}

func TestDecisionMakingSellAndHold(t *testing.T) {
	timestamp := time.Date(2026, time.January, 0, 0, 0, 0, 0, time.UTC)

	indicator := Indicator{
		VWAP:              100,
		StandardDeviation: 2,
		UpperBand:         102,
		LowerBand:         98,
	}
	tests := []struct {
		name       string
		position   account.Position
		wantAction string
	}{
		{
			name: "sell at stop loss",
			position: account.Position{
				Symbol:          "AAPL",
				Quantity:        10,
				CurrentPrice:    94,
				StopLossPrice:   95,
				TakeProfitPrice: 110,
			},
			wantAction: "Sell",
		},
		{
			name: "sell at take profit",
			position: account.Position{
				Symbol:          "AAPL",
				Quantity:        10,
				CurrentPrice:    111,
				StopLossPrice:   95,
				TakeProfitPrice: 110,
			},
			wantAction: "Sell",
		},
		{
			name: "hold without positon",
			position: account.Position{
				Symbol:       "AAPL",
				Quantity:     0,
				CurrentPrice: 100,
			},
			wantAction: "Hold",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			position := test.position
			bar := marketdata.BarTick{
				Symbol:    "AAPL",
				Timestamp: timestamp,
			}

			signal := DecisionMaking(&indicator, &position, bar)

			if signal.Action != test.wantAction {
				t.Fatalf("action: got %s, want %s", signal.Action, test.wantAction)
			}

			if test.wantAction == "Sell" {
				if signal.Symbol != "AAPL" {
					t.Fatalf("Symbol: got %s, want AAPL", signal.Symbol)
				}

				if !signal.CreatedAt.Equal(timestamp) {
					t.Fatalf("timestamp: got %v, want %v", signal.CreatedAt, timestamp)
				}
			}
		})
	}
}
