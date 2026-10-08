package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"time"

	"apexquant/internal/backtest"
	"apexquant/internal/marketdata"
	"apexquant/internal/session"
)

func main() {
	fmt.Println("Historical backtest started")

	reader := bufio.NewReader(os.Stdin)

	allocations, err := readPortfolioAllocation(reader)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Print("\n\tPortfolio: ", allocations)

	start, end, err := marketdata.CompletedDailyRange(time.Now())
	if err != nil {
		log.Fatal(err)
	}

	service, err := backtest.NewService(marketdata.NewClient(), backtest.Credentials{AlpacaKey: os.Getenv("ALPACA_API_KEY"), AlpacaSecret: os.Getenv("ALPACA_API_SECRET"), FREDKey: os.Getenv("FRED_API_KEY")}, backtest.DefaultServiceSettings())
	if err != nil {
		log.Fatal(err)
	}
	output, err := service.Run(context.Background(), backtest.RunRequest{InitialCapital: 50000, Allocations: allocations, Start: start, End: end})
	if err != nil {
		log.Fatal(err)
	}
	result := output.Result

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
			if _, err := fmt.Fscan(reader, &symbol); err != nil {
				return nil, err
			}
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
			if _, err := fmt.Fscan(reader, &percent); err != nil {
				return nil, err
			}
			if math.IsNaN(percent) || math.IsInf(percent, 0) || percent <= 0 || percent > percentageLeft {
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
