package risk

import (
	"apexquant/internal/account"
	"apexquant/internal/simulation"
	"math"
)

// Put kelly, position sizing, risk
func KellyCriterion(result simulation.MonteCarloResult, position account.Position) float64 {

	winAmount := position.TakeProfitPrice - position.EntryPrice
	lossAmount := position.EntryPrice - position.StopLossPrice
	numerator := (winAmount/lossAmount)*result.WinProbability - result.LossProbability
	if lossAmount == 0 {
		return 0
	}
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
	if riskPerShare == 0 {
		return 0
	}
	return dollarRisk / riskPerShare
}

func EvaluateRisk(result simulation.MonteCarloResult, position account.Position, acc account.Account) float64 {
	kellyFraction := KellyCriterion(result, position)
	riskPerShare := RiskPerShare(position)
	dollarRisk := DollarRisk(acc, kellyFraction)
	quantity := Quantity(riskPerShare, dollarRisk)

	return quantity
}
