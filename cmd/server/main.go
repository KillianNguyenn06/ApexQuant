package main

import (
	"fmt"

	"apexquant/internal/account"
	"apexquant/internal/algorithm"
	"apexquant/internal/mockdata"
	"apexquant/internal/session"
	"apexquant/internal/simulation"
)

func main() {
	fmt.Println("Server started")

	position := mockdata.InitialPosition
	acc := mockdata.InitialAccount
	monteCarloInput := mockdata.MonteCarloInput
	vwapState := algorithm.VWAPState{}
	indicator := algorithm.Indicator{}
	result := simulation.MonteCarloResult{}

	for i := 0; i < len(mockdata.Bars)-1; i++ {
		bar := mockdata.Bars[i]
		nextBar := mockdata.Bars[i+1]
		position.CurrentPrice = bar.Close

		algorithm.VWAP(bar, &vwapState, &indicator)
		algorithm.StandardDeviation(bar, &vwapState, &indicator)
		signal := algorithm.DecisionMaking(&indicator, &position)

		fmt.Printf(
			"\n%s close=%.2f vwap=%.2f lower=%.2f upper=%.2f signal=%s\n",
			bar.Timestamp.Format("15:04"),
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
	fmt.Printf("\n>>> Current Buying Power: %.2f\nCurrent Cash: %.2f\nCurrent Equity: %.2f", acc.BuyingPower, acc.Cash, acc.Equity)
}

// func pause() {
// 	fmt.Print("\n\tPress Enter to continue...")
// 	fmt.Scanln()
// 	fmt.Print("\033[H\033[2J")
// }

//Aug 5th:
/*
- Provide Mock Data/ test Pipeline in main
- Add Equity/Cash control in Session and adjust flow in Session
- Adjust Risk Evaluation to be based on available fund
- Add return Order for SubmitOrder and add FillOrderAtNextBar */
