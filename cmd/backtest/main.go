package main

import (
	"bufio"
	"context"
	"flag"
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
	timeframe := flag.String("timeframe", "1Day", "Execution bars: 1Day (minute execution temporarily disabled)")
	holdingDays := flag.Int("holding-days", 5, "Maximum holding period in trading days, including entry day")
	model := flag.String("probability-model", "gbm", "Probability model: gbm (Off), vwap_reversion_auto (Auto), vwap_reversion (Manual)")
	halfLife := flag.Float64("reversion-half-life", 5, "VWAP reversion half-life in trading days (only for vwap_reversion)")
	vwapWindow := flag.Int("vwap-window", 20, "Daily rolling VWAP window: 2 to 252 trading days")
	days := flag.Int("days", 7, "Completed calendar days to request")
	flag.Parse()
	if *timeframe == "1Min" && !backtest.MinuteExecutionEnabled {
		log.Fatal("minute execution is temporarily disabled pending disk-backed histories and bounded replay; use -timeframe 1Day")
	}
	if *days < 1 || (*timeframe != "1Min" && *timeframe != "1Day") {
		log.Fatal("use positive days and timeframe 1Min or 1Day")
	}
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

	loc, _ := time.LoadLocation("America/New_York")
	start = end.In(loc).Add(time.Second).AddDate(0, 0, -*days).UTC()
	service, err := backtest.NewService(marketdata.NewClient(), backtest.Credentials{AlpacaKey: os.Getenv("ALPACA_API_KEY"), AlpacaSecret: os.Getenv("ALPACA_API_SECRET"), FREDKey: os.Getenv("FRED_API_KEY")}, backtest.DefaultServiceSettings())
	if err != nil {
		log.Fatal(err)
	}
	dailyWindow := 0
	if *timeframe == "1Day" {
		dailyWindow = *vwapWindow
	}
	output, err := service.Run(context.Background(), backtest.RunRequest{HoldingDays: *holdingDays, ProbabilityModel: *model, ReversionHalfLifeDays: func() float64 {
		if *model == "vwap_reversion" {
			return *halfLife
		}
		return 0
	}(), DailyVWAPWindow: dailyWindow, Timeframe: *timeframe, InitialCapital: 50000, Allocations: allocations, Start: start, End: end})
	if err != nil {
		log.Fatal(err)
	}
	result := output.Result

	for _, snapshot := range result.Snapshots {
		fmt.Printf(
			"\n%s %s close=%.2f vwap=%.2f lower=%.2f upper=%.2f signal=%s",
			snapshot.Timestamp.In(loc).Format("2006-01-02 15:04 MST"),
			snapshot.Bar.Symbol,
			snapshot.Bar.Close,
			snapshot.Indicator.VWAP,
			snapshot.Indicator.LowerBand,
			snapshot.Indicator.UpperBand,
			snapshot.Signal.Action,
		)

		fmt.Printf(" reason=%s execution=%s EMA20=%.2f", snapshot.DecisionReason, snapshot.ExecutionReason, snapshot.Indicator.EMA)
		if snapshot.Risk != nil {
			fmt.Printf(" win=%.4f loss=%.4f timeout=%.4f Kelly=%.4f", snapshot.Risk.WinProbability, snapshot.Risk.LossProbability, snapshot.Risk.TimeoutProbability, snapshot.Risk.KellyFraction)
		}
		fills := snapshot.FilledOrders
		if len(fills) == 0 && snapshot.FilledOrder != nil {
			fills = append(fills, *snapshot.FilledOrder)
		}
		for _, order := range fills {

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
