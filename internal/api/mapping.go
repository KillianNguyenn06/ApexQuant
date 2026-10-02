package api

import (
	"encoding/json"
	"strings"

	"apexquant/internal/account"
	"apexquant/internal/backtest"
)

/*
NewResultResponse maps a complete chronological result. It must receive the
full snapshot history, not a page: indicator availability depends on earlier
volume for each symbol. The returned DTO owns its maps, slices, and pointers.
*/
func NewResultResponse(id string, request CreateBacktestRequest, config backtest.BacktestConfig, data DataInfo, result backtest.BacktestResult) (BacktestResultResponse, error) {
	// Do not hide invalid engine numbers by mapping them to null.
	if _, err := json.Marshal(result); err != nil {
		return BacktestResultResponse{}, &APIError{
			Code: "invalid_result",
			Message: "Backtest result contains values that cannot be encoded as JSON.",
		}
	}
	request.Allocations = append([]Allocation{}, request.Allocations...)
	if data.RiskFreeRateDate != nil {
		date := *data.RiskFreeRateDate
		data.RiskFreeRateDate = &date
	}
	response := BacktestResultResponse{
		ID: id, Request: request, Data: data,
		Settings:     SimulationSettings{
			VolatilityWindow: config.VolatilityWindow,
			PeriodsPerYear: config.PeriodsPerYear,
			InitialVolatility: config.MonteCarloInput.Volatility,
			TimeHorizon: config.MonteCarloInput.TimeHorizon,
			NumPaths: config.MonteCarloInput.NumPaths,
			NumSteps: config.MonteCarloInput.NumSteps,
			Seed: config.MonteCarloInput.Seed,
		},
		FinalAccount: mapAccount(result.FinalAccount),
		FinalPositions: make(map[string]Position, len(result.FinalPositions)),
		Snapshots: make([]Snapshot, 0, len(result.Snapshots)),
	}
	for symbol, p := range result.FinalPositions {
		response.FinalPositions[symbol] = mapPosition(p)
	}
	hasVolume := map[string]bool{}
	for _, s := range result.Snapshots {
		b := s.Bar
		bar := Bar{
			Symbol: b.Symbol,
			Open: b.Open,
			High: b.High,
			Low: b.Low,
			Close: b.Close,
			Volume: b.Volume,
			Timestamp: b.Timestamp.UTC(),
		}
		if b.Volume > 0 {
			hasVolume[b.Symbol] = true
			bar.VWAP = number(b.VWAP)
		}
		indicators := Indicators{}
		if hasVolume[b.Symbol] {
			indicators = Indicators{
				VWAP: number(s.Indicator.VWAP),
				StandardDeviation: number(s.Indicator.StandardDeviation),
				UpperBand: number(s.Indicator.UpperBand),
				LowerBand: number(s.Indicator.LowerBand),
			}
		}
		action := strings.ToLower(s.Signal.Action)
		if action != "buy" && action != "sell" && action != "hold" {
			return BacktestResultResponse{}, &APIError{
				Code: "invalid_result",
				Message: "Backtest returned an unknown signal action.",
			}
		}
		signal := Signal{
			Symbol: b.Symbol,
			Action: action,
			CreatedAt: s.Timestamp.UTC(),
		}
		if action != "hold" {
			signal.Price = number(s.Signal.Price)
		}
		response.Snapshots = append(response.Snapshots, Snapshot{
			Timestamp: s.Timestamp.UTC(),
			Bar: bar,
			Indicator: indicators,
			Signal: signal,
			Position: mapPosition(s.Position),
			Account: mapAccount(s.Account),
			SubmittedOrder: mapOrder(s.SubmittedOrder),
			FilledOrder: mapOrder(s.FilledOrder),
		})
	}
	if _, err := json.Marshal(response); err != nil {
		return BacktestResultResponse{}, &APIError{
			Code: "invalid_result",
			Message: "Backtest metadata contains values that cannot be encoded as JSON.",
		}
	}
	return response, nil
}
func number(value float64) *float64 {
	return &value
}
func mapAccount(a account.Account) Account {
	return Account{
		Equity: a.Equity,
		Cash: a.Cash,
		BuyingPower: a.BuyingPower,
	}
}
func mapPosition(p account.Position) Position {
	out := Position{
		Symbol: p.Symbol,
		Quantity: p.Quantity,
		CurrentPrice: p.CurrentPrice,
	}
	if p.Quantity > 0 {
		out.EntryPrice = number(p.EntryPrice)
		out.StopLossPrice = number(p.StopLossPrice)
		out.TakeProfitPrice = number(p.TakeProfitPrice)
	}
	return out
}
func mapOrder(o *account.Order) *Order {
	if o == nil {
		return nil
	}
	out := &Order{
		ID: o.ID,
		Symbol: o.Symbol,
		Action: strings.ToLower(o.Action),
		Quantity: o.Quantity,
		Status: strings.ToLower(o.Status),
		CreatedAt: o.CreatedAt.UTC(),
	}
	if out.Status == "filled" {
		out.FilledPrice = number(o.FilledPrice)
	}
	return out
}
