package risk

import (
	"apexquant/internal/account"
	"math"
)

// Put kelly, position sizing, risk
func KellyCriterion(winProbability, lossProbability, winAmount, lossAmount float64) float64 {

	numerator := (winAmount/lossAmount)*winProbability - lossProbability
	denominator := winAmount / lossAmount
	fraction := numerator / denominator
	if fraction < 0 {
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

func Quantity(riskPerShare float64, dollarRisk float64) float64 {
	return dollarRisk / riskPerShare
}
