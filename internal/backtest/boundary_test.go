package backtest

import (
	"encoding/json"
	"math"
	"testing"

	"apexquant/internal/account"
	"apexquant/internal/marketdata"
	"apexquant/internal/session"
	"apexquant/internal/simulation"
)

func boundaryConfig() BacktestConfig {
	return BacktestConfig{InitialAccount: account.Account{Cash: 10000, Equity: 10000, BuyingPower: 10000}, Allocations: []session.PortfolioAllocation{{Symbol: "AAPL", Weight: 1}}, BarsBySymbol: map[string][]marketdata.BarTick{"AAPL": {testBar("AAPL", 0, 100, 100, 100), testBar("AAPL", 1, 100, 100, 100), testBar("AAPL", 2, 100, 100, 100)}}, MonteCarloInput: simulation.MonteCarlo{Volatility: .2, TimeHorizon: 5.0 / 252, NumPaths: 100, NumSteps: 10, Seed: 42}, VolatilityWindow: 20, PeriodsPerYear: 252}
}
func TestBoundaryRejections(t *testing.T) {
	for name, change := range map[string]func(*BacktestConfig){
		"normalized duplicate": func(c *BacktestConfig) {
			c.Allocations = []session.PortfolioAllocation{{Symbol: " aapl ", Weight: .5}, {Symbol: "AAPL", Weight: .5}}
		},
		"nan weight":       func(c *BacktestConfig) { c.Allocations[0].Weight = math.NaN() },
		"infinite capital": func(c *BacktestConfig) { c.InitialAccount.Cash = math.Inf(1) },
		"nan horizon":      func(c *BacktestConfig) { c.MonteCarloInput.TimeHorizon = math.NaN() },
		"unordered":        func(c *BacktestConfig) { b := c.BarsBySymbol["AAPL"]; b[0], b[1] = b[1], b[0] },
		"negative price":   func(c *BacktestConfig) { c.BarsBySymbol["AAPL"][0].Close = -1 },
		"mismatched timelines": func(c *BacktestConfig) {
			c.Allocations = []session.PortfolioAllocation{{Symbol: "AAPL", Weight: .5}, {Symbol: "MSFT", Weight: .5}}
			c.BarsBySymbol["MSFT"] = []marketdata.BarTick{testBar("MSFT", 0, 100, 100, 100), testBar("MSFT", 1, 100, 100, 100), testBar("MSFT", 3, 100, 100, 100)}
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := boundaryConfig()
			change(&c)
			if _, err := RunBacktest(c); err == nil {
				t.Fatal("accepted invalid config")
			}
		})
	}
}
func TestZeroVolumeAndNormalizedInput(t *testing.T) {
	c := boundaryConfig()
	c.Allocations[0].Symbol = " aapl "
	for i := range c.BarsBySymbol["AAPL"] {
		c.BarsBySymbol["AAPL"][i].Volume = 0
		c.BarsBySymbol["AAPL"][i].VWAP = 0
	}
	result, err := RunBacktest(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := json.Marshal(result); err != nil {
		t.Fatalf("non-finite result: %v", err)
	}
	for _, s := range result.Snapshots {
		if s.SubmittedOrder != nil || s.FilledOrder != nil || s.Indicator.StandardDeviation != 0 {
			t.Fatal("zero-volume bar traded or invalid SD")
		}
	}
	if c.Allocations[0].Symbol != " aapl " {
		t.Fatal("engine mutated input")
	}
}

func TestFutureBarsDoNotChangeEarlierSnapshots(t *testing.T) {
	c := boundaryConfig()
	before, err := RunBacktest(c)
	if err != nil {
		t.Fatal(err)
	}
	c.BarsBySymbol["AAPL"][2] = testBar("AAPL", 2, 1000, 1000, 1000)
	after, err := RunBacktest(c)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		a, _ := json.Marshal(before.Snapshots[i])
		b, _ := json.Marshal(after.Snapshots[i])
		if string(a) != string(b) {
			t.Fatal("future bar changed earlier snapshot")
		}
	}
}
