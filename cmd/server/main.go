package main

import (
	"bufio"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"time"

	"apexquant/internal/backtest"
	"apexquant/internal/marketdata"
	"apexquant/internal/mockdata"
	"apexquant/internal/session"
)

func main() {
	fmt.Println("Server started")

	reader := bufio.NewReader(os.Stdin)

	allocations, err := readPortfolioAllocation(reader)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Print("\n\tPortfolio: ", allocations)

	end := time.Now().UTC()
	start := end.AddDate(-1, 0, 0)

	barsBySymbol := make(
		map[string][]marketdata.BarTick,
		len(allocations),
	)

	for _, allocation := range allocations {
		bars, err := marketdata.FetchAPI(allocation.Symbol, start, end, os.Getenv("APCA_API_KEY_ID"), os.Getenv("APCA_API_SECRET_KEY"))
		if err != nil {
			log.Fatal(err)
		}

		barsBySymbol[allocation.Symbol] = bars

		fmt.Printf("\n\tLoaded %d bars for %s with %.2f%% allocation", len(bars), allocation.Symbol, allocation.Weight*100)
	}

	riskFreeRate, err := marketdata.FetchRiskFreeRate(
		os.Getenv("FRED_API_KEY"),
		time.Now(),
	)
	if err != nil {
		log.Fatal(err)
	}

	monteCarloInput := mockdata.MonteCarloInput
	monteCarloInput.RiskFreeRate = riskFreeRate

	result, err := backtest.RunBacktest(
		backtest.BacktestConfig{
			InitialAccount:   mockdata.InitialAccount,
			Allocations:      allocations,
			BarsBySymbol:     barsBySymbol,
			MonteCarloInput:  monteCarloInput,
			VolatilityWindow: 20,
			PeriodsPerYear:   252,
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	for _, snapshot := range result.Snapshots {
		fmt.Printf(
			"\n%s %s close=%.2f vwap=%.2f lower=%.2f upper=%.2f signal=%s",
			snapshot.Timestamp.Format("2006-01-02"),
			snapshot.Bar.Symbol,
			snapshot.Bar.Close,
			snapshot.Indicator.VWAP,
			snapshot.Indicator.LowerBand,
			snapshot.Indicator.UpperBand,
			snapshot.Signal.Action,
		)

		if snapshot.FilledOrder != nil {
			order := snapshot.FilledOrder

			fmt.Printf(
				"\n===> FILLED order id=%s action=%s quantity=%.2f filled_price=%.2f",
				order.ID,
				order.Action,
				order.Quantity,
				order.FilledPrice,
			)
		}

		if snapshot.SubmittedOrder != nil {
			order := snapshot.SubmittedOrder

			fmt.Printf(
				"\n===> SUBMITTED order id=%s action=%s quantity=%.2f status=%s",
				order.ID,
				order.Action,
				order.Quantity,
				order.Status,
			)
		}
	}

	fmt.Printf(
		"\n\n>>> Final Buying Power: %.2f\nFinal Cash: %.2f\nFinal Equity: %.2f\n",
		result.FinalAccount.BuyingPower,
		result.FinalAccount.Cash,
		result.FinalAccount.Equity,
	)
}

// =================================================
// Read Tickers Allocation
// =================================================
func readPortfolioAllocation(reader *bufio.Reader) ([]session.PortfolioAllocation, error) {
	var stockCount int

	fmt.Print("\n\tHow many stocks (1-8): ")
	if _, err := fmt.Fscan(reader, &stockCount); err != nil {
		return nil, err
	}
	if stockCount < 1 || stockCount > 8 {
		return nil, fmt.Errorf("\n\tError: Number of ticker must be between 1-8.\n")
	}

	allocations := make([]session.PortfolioAllocation, 0, stockCount)
	usedSymbol := make(map[string]bool)
	totalWeight := 0.0
	percentageLeft := 100.00

	for i := 0; i < stockCount; i++ {
		var symbol string
		var percent float64

		for {
			isUsed := false
			fmt.Printf("\n\tStock ticker #%d:", i+1)
			fmt.Fscan(reader, &symbol)
			symbol = strings.ToUpper(strings.TrimSpace(symbol))
			if usedSymbol[symbol] {
				fmt.Printf("\n\tError: Ticker %v already been selected.\n", symbol)
				isUsed = true
				continue
			}
			if !isUsed {
				break
			}
		}
		for {
			isValid := true
			fmt.Printf("\n\t%s allocation percentage (Total allocation left %.2f%%): ", symbol, percentageLeft)
			fmt.Fscan(reader, &percent)
			if percent <= 0 || percent > percentageLeft {
				isValid = false
				fmt.Printf("\n\tError: Allocation percentage must be between 0 and %.2f%%\n", percentageLeft)
				continue
			} else if i == stockCount-1 && math.Abs(percent-percentageLeft) > 0.000001 {
				isValid = false
				fmt.Printf("\n\t%s must use the remaining %.2f%%\n", symbol, percentageLeft)
				continue
			} else if i < stockCount-1 && percent >= percentageLeft {
				isValid = false
				fmt.Printf("\n\tError: Allocation must leave some percentage for the remaining stocks.\n")
				continue
			}
			if isValid {
				break
			}
		}
		weight := percent / 100
		allocations = append(allocations, session.PortfolioAllocation{
			Symbol: symbol,
			Weight: weight,
		})
		usedSymbol[symbol] = true
		totalWeight += weight
		percentageLeft -= percent
	}
	return allocations, nil
}

// func pause() {
// 	fmt.Print("\n\tPress Enter to continue...")
// 	fmt.Scanln()
// 	fmt.Print("\033[H\033[2J")
// }

//Aug 6th:
/*
 - Add/Fetch 2 months API
 - Fix how Cash, Equity, Buying Power are being calculated in Session
 - Add validation in KellyCriterion
 + What might take into consideration:
 Currently API is fetching 1 bar/day not minute
 Pagination is not implemented.
 If Alpaca returns a next_page_token, additional pages will not be fetched.
 This is not a problem for two months of daily bars, but it matters for long ranges or minute bars.
 Adjustment=raw can make long-term backtests misleading around stock splits and dividends.
 There are couple more data that should consider fetching instead of hard-coding such as VWAP, Risk free rate, volatility?
*/

//Aug 7th
/*
- Did quite a lot including restructuring Session, main.go as well as added fetch Risk free rate from FRED to prepare for UI
- Already done fetching VWAP, RFR as well as calculate Volatility in Aug 6th
- Changed from fetching 1 symbol into multiple symbols using slice, as well as added weight in symbols
- main.go, Session changed a lot, add Pending Filled Order as well in Session, thus need some review */
