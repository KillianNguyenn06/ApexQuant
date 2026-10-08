package backtest

import (
	"context"
	"math"
	"testing"

	"apexquant/internal/marketdata"
	"apexquant/internal/session"
)

func TestVolatilityHistoryRollingWindowAndNoLookahead(t *testing.T) {
	c := boundaryConfig()
	c.VolatilityWindow = 3
	c.VolatilityHistory = map[string][]marketdata.BarTick{"AAPL": {
		testBar("AAPL", -3, 50, 50, 50),
		testBar("AAPL", -2, 50, 50, 50),
		testBar("AAPL", -1, 100, 100, 100),
	}}
	// First day's rolling window is [50, 100, 100]: returns log(2), 0.
	want := math.Log(2) / math.Sqrt(2) * math.Sqrt(c.PeriodsPerYear)
	got, err := volatilityAt(c, "AAPL", 0)
	if err != nil || math.Abs(got-want) > 1e-12 {
		t.Fatalf("first-day volatility=%v, want %v: %v", got, want, err)
	}
	c.BarsBySymbol["AAPL"][2] = testBar("AAPL", 2, 10000, 10000, 10000)
	again, _ := volatilityAt(c, "AAPL", 0)
	if again != got {
		t.Fatal("future prices changed first-day volatility")
	}
	// On day two the window is [100, 100, 100], so history has rolled out.
	second, err := volatilityAt(c, "AAPL", 1)
	if err != nil || second != 0 {
		t.Fatalf("second-day volatility=%v: %v", second, err)
	}
	c.BarsBySymbol["MSFT"] = []marketdata.BarTick{testBar("MSFT", 0, 200, 200, 200)}
	other, _ := volatilityAt(c, "MSFT", 0)
	if other != c.MonteCarloInput.Volatility {
		t.Fatal("a symbol without history inherited another symbol's estimate")
	}
}

func TestServiceVolatilityPreparationDoesNotTrade(t *testing.T) {
	p := &fakeProvider{rate: .04, bars: map[string][]marketdata.BarTick{}}
	for _, symbol := range []string{"AAPL", "MSFT"} {
		for i := -30; i < 4; i++ {
			price := 100.0
			if i < 0 {
				price = 400 + float64(i%3)*10
			}
			p.bars[symbol] = append(p.bars[symbol], testBar(symbol, i, price, price, price))
		}
	}
	settings := DefaultServiceSettings()
	settings.MonteCarlo.NumPaths, settings.MonteCarlo.Seed = 10, 42
	s, err := NewService(p, Credentials{}, settings)
	if err != nil {
		t.Fatal(err)
	}
	r := RunRequest{InitialCapital: 50000, Allocations: []session.PortfolioAllocation{{Symbol: "AAPL", Weight: .5}, {Symbol: "MSFT", Weight: .5}}, Start: testBar("AAPL", 0, 100, 100, 100).Timestamp, End: testBar("AAPL", 3, 100, 100, 100).Timestamp}
	out, err := s.Run(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Result.Snapshots) != 8 || len(out.Config.VolatilityHistory["AAPL"]) != 20 || len(out.Config.BarsBySymbol["AAPL"]) != 4 {
		t.Fatal("preparation changed the trading range or exceeded its history budget")
	}
	for _, snap := range out.Result.Snapshots {
		if snap.Timestamp.Before(r.Start) || snap.Account.Cash != 50000 || snap.Account.Equity != 50000 || snap.Position.Quantity != 0 || snap.SubmittedOrder != nil || snap.FilledOrder != nil {
			t.Fatal("preparation created trades or changed capital")
		}
		if snap.Indicator.VWAP != 100 {
			t.Fatal("preparation prices entered trading indicators")
		}
	}
	first, err := volatilityAt(out.Config, "AAPL", 0)
	if err != nil || first == settings.MonteCarlo.Volatility {
		t.Fatal("first trading day still used the fallback")
	}
}

func TestRejectVolatilityHistoryOnTradingDay(t *testing.T) {
	c := boundaryConfig()
	c.VolatilityHistory = map[string][]marketdata.BarTick{"AAPL": {c.BarsBySymbol["AAPL"][0]}}
	if _, err := RunBacktest(c); err == nil {
		t.Fatal("accepted preparation history from the trading period")
	}
}
