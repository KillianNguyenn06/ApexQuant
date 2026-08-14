package main

import (
	"bufio"
	"fmt"
	"log"
	"math"
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

	allocations, err := readPortfolioAllocation(reader)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print("\n\tPortfolio: ", allocations)

	end := time.Now().UTC()
	start := end.AddDate(-1, 0, 0)

	tradingSession := session.TradingSession{
		StartingCapital: mockdata.InitialAccount.Equity,
		Cash:            mockdata.InitialAccount.Cash,
		Symbols:         make(map[string]*session.SymbolState),
		StartedAt:       time.Now(),
	}

	for _, allocation := range allocations {
		bars, err := marketdata.FetchAPI(allocation.Symbol, start, end, os.Getenv("APCA_API_KEY_ID"), os.Getenv("APCA_API_SECRET_KEY"))
		if err != nil {
			log.Fatal(err)
		}
		if len(bars) < 2 {
			log.Fatal("not enough bars returned")
		}

		position := mockdata.InitialPosition
		position.Symbol = allocation.Symbol
		tradingSession.Symbols[allocation.Symbol] = &session.SymbolState{
			Bars:             bars,
			Positions:        &position,
			AllocationWeight: allocation.Weight,
		}

		fmt.Printf("\n\tLoaded %d bars for %s with %.2f%% allocation", len(bars), allocation.Symbol, allocation.Weight*100)
	}

	riskFreeRate, err := marketdata.FetchRiskFreeRate(os.Getenv("FRED_API_KEY"), time.Now())
	if err != nil {
		log.Fatal(err)
	}

	acc := mockdata.InitialAccount
	monteCarloInput := mockdata.MonteCarloInput
	monteCarloInput.RiskFreeRate = riskFreeRate

	referenceSymbol := allocations[0].Symbol
	referenceBars := tradingSession.Symbols[referenceSymbol].Bars

	for _, allocation := range allocations[1:] {
		symbol := allocation.Symbol
		bars := tradingSession.Symbols[symbol].Bars

		if len(bars) != len(referenceBars) {
			log.Fatalf("\n\tError: Bar count mismatch: %s has %d bars but %s has %d bars", referenceSymbol, len(referenceBars), symbol, len(bars))
		}

		for i := range referenceBars {
			referenceDate := referenceBars[i].Timestamp.Format("2006-01-02")
			symbolDate := bars[i].Timestamp.Format("2006-01-02")

			if referenceDate != symbolDate {
				log.Fatalf("\n\tError: Bar date mismatch at index %d: %s=%s, %s=%s", i, referenceSymbol, referenceDate, symbol, symbolDate)
			}
		}
	}

	barCount := len(referenceBars)

	snapshots := make([]session.BacktestSnapshot, 0, barCount*len(tradingSession.Symbols)) // Last element create capacity of Processed Day * Num of Symbol
	for i := 0; i < barCount; i++ {

		filledOrders := make(map[string]*account.Order)

		// 1. Update every symbol to today's opening price
		for _, allocation := range allocations {
			state := tradingSession.Symbols[allocation.Symbol]
			state.Positions.CurrentPrice = state.Bars[i].Open
		}

		// 2. Calculate portfolio equity at today's Open
		acc.Equity = acc.Cash

		for _, allocation := range allocations {
			state := tradingSession.Symbols[allocation.Symbol]
			positionValue := state.Positions.Quantity * state.Positions.CurrentPrice
			acc.Equity += positionValue
		}

		// 3. Fill orders submitted during the previous bar
		for _, allocation := range allocations {
			state := tradingSession.Symbols[allocation.Symbol]
			bar := state.Bars[i]
			if state.PendingOrder == nil {
				continue
			}
			filledOrder, filled := session.FillPendingOrder(
				*state.PendingOrder,
				state.Positions,
				&acc,
				bar,
				state.AllocationWeight,
			)

			if filled {
				filledOrderCopy := filledOrder
				filledOrders[allocation.Symbol] = &filledOrderCopy
				fmt.Printf("\n===> FILLED order id=%s action=%s quantity=%.2f filled_price=%.2f\n", filledOrder.ID, filledOrder.Action, filledOrder.Quantity, filledOrder.FilledPrice)
			}
			state.PendingOrder = nil
		}

		// 4. Updated every symbol to today's closing price
		for _, allocation := range allocations {
			state := tradingSession.Symbols[allocation.Symbol]
			state.Positions.CurrentPrice = state.Bars[i].Close
		}

		// 5. Calculate portfolio equity at today's close
		acc.Equity = acc.Cash
		for _, allocation := range allocations {
			state := tradingSession.Symbols[allocation.Symbol]
			positionValue := state.Positions.Quantity * state.Positions.CurrentPrice
			acc.Equity += positionValue
		}

		// 6. Calculate signals for every symbols
		for _, allocation := range allocations {

			state := tradingSession.Symbols[allocation.Symbol]
			bars := state.Bars
			bar := bars[i]

			/*	Have to evaluate availableBar else if call the whole function for 1-year, it causes look-ahead bias
				Because Volatility gets the whole 1-year data instead of just whats available during the run */
			startIndex := max(0, i-19)
			availableBars := bars[startIndex : i+1]

			if len(availableBars) >= 3 {
				volatility, err := simulation.AnnualizedVolatility(availableBars, 252)
				if err != nil {
					log.Fatal(err)
				}
				monteCarloInput.Volatility = volatility
			}
			algorithm.VWAP(bar, &state.VWAP, &state.Indicators)
			algorithm.StandardDeviation(bar, &state.VWAP, &state.Indicators)
			signal := algorithm.DecisionMaking(&state.Indicators, state.Positions, bar)

			fmt.Printf("\n%s %s close=%.2f vwap=%.2f lower=%.2f upper=%.2f signal=%s\n", bar.Timestamp.Format("2006-01-02"), allocation.Symbol, bar.Close, state.Indicators.VWAP, state.Indicators.LowerBand, state.Indicators.UpperBand, signal.Action)

			var submittedOrder *account.Order

			// The final bar can fill orders but cannot submit a new order.
			if i < barCount-1 {
				order, submitted := session.Session(
					signal,
					monteCarloInput,
					state.Positions,
					simulation.MonteCarloResult{},
					&acc,
					&account.Order{},
					state.AllocationWeight,
				)

				if submitted {
					orderCopy := order
					state.PendingOrder = &orderCopy
					submittedOrder = &orderCopy

					fmt.Printf("\n===> SUBMITTED order id=%s action=%s quantity=%.2f status=%s\n", order.ID, order.Action, order.Quantity, order.Status)
				}
			}

			snapshot := session.BacktestSnapshot{
				Timestamp:      bar.Timestamp,
				Bar:            bar,
				Indicator:      state.Indicators,
				Signal:         signal,
				Position:       *state.Positions,
				Account:        acc,
				SubmittedOrder: submittedOrder,
				FilledOrder:    filledOrders[allocation.Symbol],
			}

			snapshots = append(snapshots, snapshot)
		}

		fmt.Printf("\n>>> Current Buying Power: %.2f\nCurrent Cash: %.2f\nCurrent Equity: %.2f\n", acc.BuyingPower, acc.Cash, acc.Equity)
	}
}

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
			fmt.Printf("\n\t%s allocation percentage (Total allocation left %.2f%%\n): ", symbol, percentageLeft)
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
