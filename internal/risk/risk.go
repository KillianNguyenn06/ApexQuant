package risk

import (
	"apexquant/internal/account"
	"apexquant/internal/simulation"
	"fmt"
	"math"
)

// =================================================
// Kelly Criterion Fraction
// =================================================
func KellyCriterion(result simulation.MonteCarloResult, position account.Position) float64 {

	if position.StopLossPrice >= position.EntryPrice || position.TakeProfitPrice <= position.EntryPrice || position.EntryPrice <= 0 {
		fmt.Printf("\n\tError: Stop loss must be below entry price.")
		return 0
	}
	// Maximize mean log(1+f*payoff), where f is the fraction of equity
	// exposed to one planned stop loss. Include every timeout's terminal payoff.
	if len(result.Payoffs) > 0 {
		upper := 1.0
		for _, r := range result.Payoffs {
			if math.IsNaN(r) || math.IsInf(r, 0) {
				return 0
			}
			if r < 0 {
				upper = min(upper, (1-1e-12)/(-r))
			}
		}
		derivative := func(f float64) float64 {
			sum := 0.0
			for _, r := range result.Payoffs {
				sum += r / (1 + f*r)
			}
			return sum / float64(len(result.Payoffs))
		}
		if derivative(0) <= 0 {
			return 0
		}
		if derivative(upper) >= 0 {
			return upper
		}
		lo, hi := 0.0, upper
		for i := 0; i < 50; i++ {
			mid := (lo + hi) / 2
			if derivative(mid) > 0 {
				lo = mid
			} else {
				hi = mid
			}
		}
		return (lo + hi) / 2
	}
	// Compatibility for analytical binary callers:
	// unspecified timeout payoffs are assumed zero, explicitly included in the expected-log objective.
	b := (position.TakeProfitPrice - position.EntryPrice) / (position.EntryPrice - position.StopLossPrice)
	p, q := result.WinProbability, result.LossProbability
	if p < 0 || q < 0 || p+q > 1+1e-9 || p+q <= 0 {
		return 0
	}
	fraction := (b*p - q) / (b * (p + q))
	if math.IsNaN(fraction) || math.IsInf(fraction, 0) || fraction < 0 {
		return 0
	}
	return min(1, fraction)
}

// =================================================
// Risk Per Share Calculation
// =================================================
func RiskPerShare(position account.Position) float64 {
	return math.Abs(position.EntryPrice - position.StopLossPrice)
}

// =================================================
// Total Dollar Risk Calculation
// =================================================
func DollarRisk(position account.Account, kellyFraction float64) float64 {
	return position.Equity * kellyFraction
}

// =================================================
// Order's quantity Calculation
// =================================================
func QuantityByRisk(riskPerShare float64, dollarRisk float64) float64 {
	if riskPerShare == 0 {
		fmt.Printf("\n\tRisk Per Share is low: %v", riskPerShare)
		return 0
	}
	return dollarRisk / riskPerShare
}

// =================================================
// Evaluate Risk
// =================================================
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
