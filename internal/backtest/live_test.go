package backtest

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"testing"
	"time"

	"apexquant/internal/account"
	"apexquant/internal/marketdata"
	"apexquant/internal/session"
	"apexquant/internal/simulation"
)

// Explicit opt-in: ordinary tests never need credentials or a network connection.
func TestLivePortfolioReconciliation(t *testing.T) {
	if os.Getenv("APEXQUANT_LIVE_TEST") != "1" {
		t.Skip("set APEXQUANT_LIVE_TEST=1 and provider credentials to run")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	start := time.Date(2025, 9, 5, 4, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 5, 3, 59, 59, 0, time.UTC)
	client := marketdata.NewClient()
	c := BacktestConfig{
		InitialAccount: account.Account{
			Cash:        50000,
			Equity:      50000,
			BuyingPower: 50000,
		},
		Allocations: []session.PortfolioAllocation{
			{Symbol: "AAPL", Weight: .5},
			{Symbol: "MSFT", Weight: .5},
		},
		BarsBySymbol: map[string][]marketdata.BarTick{},
		MonteCarloInput: simulation.MonteCarlo{
			Volatility:  .2,
			TimeHorizon: 5.0 / 252,
			NumPaths:    100000,
			NumSteps:    252,
			Seed:        42,
		},
		VolatilityWindow: 20,
		PeriodsPerYear:   252,
	}
	for _, a := range c.Allocations {
		bars, err := client.FetchBars(ctx, a.Symbol, start, end, os.Getenv("APCA_API_KEY_ID"), os.Getenv("APCA_API_SECRET_KEY"))
		if err != nil {
			t.Fatal(err)
		}
		c.BarsBySymbol[a.Symbol] = bars
		t.Logf("%s: %d bars, %s through %s", a.Symbol, len(bars), bars[0].Timestamp.Format(time.DateOnly), bars[len(bars)-1].Timestamp.Format(time.DateOnly))
	}
	rate, err := client.FetchRiskFreeRate(ctx, os.Getenv("FRED_API_KEY"), end)
	if err != nil {
		t.Fatal(err)
	}
	c.MonteCarloInput.RiskFreeRate = rate
	t.Logf("FRED rate: %.6f (constant across run; historical rate curve remains future work)", rate)
	result, err := RunBacktest(c)
	if err != nil {
		t.Fatal(err)
	}
	reconcile(t, c, result)
	if _, err := json.Marshal(result); err != nil {
		t.Fatal(err)
	}
	t.Logf("PASS: snapshots=%d cash=%.8f equity=%.8f buyingPower=%.8f positions=%+v", len(result.Snapshots), result.FinalAccount.Cash, result.FinalAccount.Equity, result.FinalAccount.BuyingPower, result.FinalPositions)
}

// Reconstruct accounting from fills, independently of the engine's account fields.
func reconcile(t *testing.T, c BacktestConfig, r BacktestResult) {
	t.Helper()
	n := len(c.Allocations)
	count := len(c.BarsBySymbol[c.Allocations[0].Symbol])
	if len(r.Snapshots) != n*count {
		t.Fatal("wrong snapshot count")
	}
	cash, bp := c.InitialAccount.Cash, c.InitialAccount.BuyingPower
	quantities := map[string]float64{}
	pending := map[string]*account.Order{}
	buys, sells, holds := 0, 0, 0
	near := func(got, want float64) {
		t.Helper()
		if math.IsNaN(got) || math.IsInf(got, 0) || math.Abs(got-want) > 1e-7*math.Max(1, math.Abs(want)) {
			t.Fatalf("got %.12f want %.12f", got, want)
		}
	}
	for i := 0; i < count; i++ {
		openingEquity := cash
		for _, a := range c.Allocations {
			openingEquity += quantities[a.Symbol] * c.BarsBySymbol[a.Symbol][i].Open
		}
		for j, a := range c.Allocations {
			s := r.Snapshots[i*n+j]
			bar := c.BarsBySymbol[a.Symbol][i]
			if s.Bar.Symbol != a.Symbol || !s.Timestamp.Equal(bar.Timestamp) {
				t.Fatal("snapshot ordering")
			}
			if o := s.FilledOrder; o != nil {
				p := pending[a.Symbol]
				if p == nil || p.ID != o.ID || !p.CreatedAt.Before(s.Timestamp) || o.Quantity <= 0 || o.Quantity > p.Quantity+1e-8 {
					t.Fatal("invalid next-bar fill")
				}
				near(o.FilledPrice, bar.Open)
				cost := o.Quantity * o.FilledPrice
				if o.Action == "Buy" {
					if cost > math.Min(cash, bp)+1e-7 || (quantities[a.Symbol]+o.Quantity)*bar.Open > openingEquity*a.Weight+1e-7 {
						t.Fatal("funding/allocation cap exceeded")
					}
					cash -= cost
					bp -= cost
					quantities[a.Symbol] += o.Quantity
					buys++
				} else if o.Action == "Sell" {
					if o.Quantity > quantities[a.Symbol]+1e-8 {
						t.Fatal("oversell")
					}
					cash += cost
					bp += cost
					quantities[a.Symbol] -= o.Quantity
					sells++
				} else {
					t.Fatal("unknown fill")
				}
			}
			pending[a.Symbol] = s.SubmittedOrder
			if i == count-1 && s.SubmittedOrder != nil {
				t.Fatal("final-bar submission")
			}
			near(s.Position.Quantity, quantities[a.Symbol])
			if s.Signal.Action == "Hold" {
				holds++
			}
		}
		equity := cash
		for _, a := range c.Allocations {
			equity += quantities[a.Symbol] * c.BarsBySymbol[a.Symbol][i].Close
		}
		for j := 0; j < n; j++ {
			s := r.Snapshots[i*n+j]
			near(s.Account.Cash, cash)
			near(s.Account.BuyingPower, bp)
			near(s.Account.Equity, equity)
		}
	}
	near(r.FinalAccount.Cash, cash)
	near(r.FinalAccount.BuyingPower, bp)
	equity := cash
	for _, a := range c.Allocations {
		near(r.FinalPositions[a.Symbol].Quantity, quantities[a.Symbol])
		equity += quantities[a.Symbol] * c.BarsBySymbol[a.Symbol][count-1].Close
	}
	near(r.FinalAccount.Equity, equity)
	t.Logf("Reconciled buy fills=%d sell fills=%d Hold snapshots=%d", buys, sells, holds)
}
