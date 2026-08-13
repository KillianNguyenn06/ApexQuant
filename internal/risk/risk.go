package risk

import (
	"apexquant/internal/account"
	"apexquant/internal/simulation"
	"fmt"
	"math"
)

// Put kelly, position sizing, risk
func KellyCriterion(result simulation.MonteCarloResult, position account.Position) float64 {

	if position.StopLossPrice >= position.EntryPrice {
		fmt.Printf("\n\tError: SL above Entry Price.")
		return 0
	}
	winAmount := position.TakeProfitPrice - position.EntryPrice
	lossAmount := position.EntryPrice - position.StopLossPrice
	numerator := (winAmount/lossAmount)*result.WinProbability - result.LossProbability
	if lossAmount == 0 {
		return 0
	}
	denominator := winAmount / lossAmount
	fraction := numerator / denominator
	if fraction < 0 {
		fmt.Printf("\n\tTrade has no positive edge: %v", fraction)
		return 0
	}
	return fraction
}

func RiskPerShare(position account.Position) float64 {
	return math.Abs(position.EntryPrice - position.StopLossPrice)
}

func DollarRisk(position account.Account, kellyFraction float64) float64 {
	return position.Equity * kellyFraction
}

func QuantityByRisk(riskPerShare float64, dollarRisk float64) float64 {
	if riskPerShare == 0 {
		fmt.Printf("\n\tRisk Per Share is low: %v", riskPerShare)
		return 0
	}
	return dollarRisk / riskPerShare
}

func EvaluateRisk(result simulation.MonteCarloResult, position account.Position, acc account.Account, allocationWeight float64) float64 {
	fractionMultiplier := 0.2 // Personal Choice of Risk Management
	kellyFraction := fractionMultiplier * KellyCriterion(result, position)
	riskPerShare := RiskPerShare(position)
	dollarRisk := DollarRisk(acc, kellyFraction)
	quantityByRisk := QuantityByRisk(riskPerShare, dollarRisk)

	symbolAllocation := acc.Equity * allocationWeight         // How much that batch of symbol weight in Equity
	symbolValue := position.CurrentPrice * position.Quantity  // How much that batch's value is worth right now
	symbolBudget := math.Max(0, symbolAllocation-symbolValue) // How much that batch allowed to grab from Account
	symbolAffordableQuantity := symbolBudget / position.EntryPrice

	availableFunds := min(acc.Cash, acc.BuyingPower)
	affordableQuantity := availableFunds / position.EntryPrice
	quantity := min(quantityByRisk, affordableQuantity, symbolAffordableQuantity)

	return quantity
}

// func pause() {
// 	fmt.Print("\n\tPress Enter to continue...")
// 	fmt.Scanln()
// 	fmt.Print("\033[H\033[2J")
// }
