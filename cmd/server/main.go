package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"apexquant/internal/account"
	"apexquant/internal/algorithm"
	"apexquant/internal/marketdata"
	"apexquant/internal/mockdata"
	"apexquant/internal/session"
	"apexquant/internal/simulation"
)

func main() {
	fmt.Println("Server started")

	reader := bufio.NewReader(os.Stdin)

	fmt.Print("\n\tEnter ticker symbol: ")
	input, err := reader.ReadString('\n')
	if err != nil {
		log.Fatal(err)
	}

	symbol := strings.ToUpper(strings.TrimSpace(input))

	end := time.Now().UTC()
	start := end.AddDate(0, -2, 0)

	bars, err := marketdata.FetchAPI(symbol, start, end, os.Getenv("APCA_API_KEY_ID"), os.Getenv("APCA_API_SECRET_KEY"))
	if err != nil {
		log.Fatal(err)
	}
	if len(bars) > 20 {
		bars = bars[len(bars)-20:]
	}
	if len(bars) < 2 {
		log.Fatal("not enough bars returned")
	}
	fmt.Printf("\n\tLoaded %d bars for %s\n", len(bars), symbol)

	position := mockdata.InitialPosition
	position.Symbol = symbol

	acc := mockdata.InitialAccount
	monteCarloInput := mockdata.MonteCarloInput
	vwapState := algorithm.VWAPState{}
	indicator := algorithm.Indicator{}
	result := simulation.MonteCarloResult{}

	for i := 0; i < len(bars)-1; i++ {
		bar := bars[i]
		nextBar := bars[i+1]
		position.CurrentPrice = bar.Close

		algorithm.VWAP(bar, &vwapState, &indicator)
		algorithm.StandardDeviation(bar, &vwapState, &indicator)
		signal := algorithm.DecisionMaking(&indicator, &position)

		fmt.Printf(
			"\n%s close=%.2f vwap=%.2f lower=%.2f upper=%.2f signal=%s\n",
			bar.Timestamp.Format("2006-01-02"),
			bar.Close,
			indicator.VWAP,
			indicator.LowerBand,
			indicator.UpperBand,
			signal.Action,
		)

		order, submitted := session.Session(
			signal,
			monteCarloInput,
			&position,
			result,
			&acc,
			&account.Order{},
			nextBar,
		)
		if submitted {
			fmt.Printf(
				"\n===> order id=%s action=%s quantity=%.2f status=%s filled_price=%.2f\n",
				order.ID,
				order.Action,
				order.Quantity,
				order.Status,
				order.FilledPrice,
			)
		}
	}
	fmt.Printf("\n>>> Current Buying Power: %.2f\nCurrent Cash: %.2f\nCurrent Equity: %.2f\n", acc.BuyingPower, acc.Cash, acc.Equity)
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
*/
