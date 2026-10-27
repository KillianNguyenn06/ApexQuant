package api

import (
	"encoding/json"
	"strings"

	"apexquant/internal/account"
	"apexquant/internal/backtest"
	"apexquant/internal/simulation"
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
			Code:    "invalid_result",
			Message: "Backtest result contains values that cannot be encoded as JSON.",
		}
	}
	data.RatePolicy = config.RatePolicy
	if config.InitialRateDate != "" {
		date := config.InitialRateDate
		data.RiskFreeRateDate = &date
	}
	request.Allocations = append([]Allocation{}, request.Allocations...)
	if data.RiskFreeRateDate != nil {
		date := *data.RiskFreeRateDate
		data.RiskFreeRateDate = &date
	}
	var estimates map[string]simulation.ReversionEstimate
	if len(config.ReversionEstimates) > 0 {
		estimates = make(map[string]simulation.ReversionEstimate, len(config.ReversionEstimates))
	}
	for symbol, fit := range config.ReversionEstimates {
		estimates[symbol] = fit
	}
	response := BacktestResultResponse{
		ID: id, Request: request, Data: data,
		Settings: SimulationSettings{
			ReversionEstimates: estimates,
			HoldingDays:        backtest.HoldingPeriod(config), ProbabilityModel: config.MonteCarloInput.Model, ReversionHalfLifeDays: config.MonteCarloInput.ReversionHalfLifeDays,
			DailyVWAPWindow:   backtest.DailyVWAPWindowSize(config),
			VolatilityWindow:  config.VolatilityWindow,
			PeriodsPerYear:    config.PeriodsPerYear,
			InitialVolatility: config.MonteCarloInput.Volatility,
			TimeHorizon:       config.MonteCarloInput.TimeHorizon,
			NumPaths:          config.MonteCarloInput.NumPaths,
			NumSteps:          config.MonteCarloInput.NumSteps,
			Seed:              config.MonteCarloInput.Seed,
		},
		FinalAccount:   mapAccount(result.FinalAccount),
		FinalPositions: make(map[string]Position, len(result.FinalPositions)),
		Snapshots:      make([]Snapshot, 0, len(result.Snapshots)),
	}
	for symbol, p := range result.FinalPositions {
		response.FinalPositions[symbol] = mapPosition(p)
	}
	hasVolume := map[string]bool{}
	for _, s := range result.Snapshots {
		b := s.Bar
		bar := Bar{
			Symbol:    b.Symbol,
			Open:      b.Open,
			High:      b.High,
			Low:       b.Low,
			Close:     b.Close,
			Volume:    b.Volume,
			Timestamp: b.Timestamp.UTC(),
		}
		if b.Volume > 0 {
			hasVolume[b.Symbol] = true
			bar.VWAP = number(b.VWAP)
		}
		indicators := Indicators{}
		if (config.Timeframe != "1Min" && hasVolume[b.Symbol]) || (config.Timeframe == "1Min" && s.Indicator.VolumeReady) {
			indicators = Indicators{
				VWAP:              number(s.Indicator.VWAP),
				StandardDeviation: number(s.Indicator.StandardDeviation),
				UpperBand:         number(s.Indicator.UpperBand),
				LowerBand:         number(s.Indicator.LowerBand),
			}
		}
		if config.Timeframe == "1Min" {
			indicators.EMA = number(s.Indicator.EMA)
			indicators.EMASlope = number(s.Indicator.EMASlope)
			indicators.EMAReady = s.Indicator.EMAReady
		}
		var filledOrders, orderEvents []Order
		for _, o := range s.FilledOrders {
			filledOrders = append(filledOrders, *mapOrder(&o))
		}
		for _, o := range s.OrderEvents {
			orderEvents = append(orderEvents, *mapOrder(&o))
		}
		var diagnostic *RiskDecision
		if s.Risk != nil {
			diagnostic = &RiskDecision{RiskFreeRate: s.Risk.RiskFreeRate, Model: s.Risk.Model, TimeoutMeanReturn: s.Risk.TimeoutMeanReturn, WinProbability: s.Risk.WinProbability, LossProbability: s.Risk.LossProbability, TimeoutProbability: s.Risk.TimeoutProbability, KellyFraction: s.Risk.KellyFraction, Quantity: s.Risk.Quantity}
		}
		action := strings.ToLower(s.Signal.Action)
		if action != "buy" && action != "sell" && action != "hold" {
			return BacktestResultResponse{}, &APIError{
				Code:    "invalid_result",
				Message: "Backtest returned an unknown signal action.",
			}
		}
		signal := Signal{
			Symbol:    b.Symbol,
			Action:    action,
			CreatedAt: s.Timestamp.UTC(),
		}
		if config.Timeframe == "1Min" {
			signal.CreatedAt = s.Signal.CreatedAt.UTC()
		}
		if action != "hold" {
			signal.Price = number(s.Signal.Price)
		}
		response.Snapshots = append(response.Snapshots, Snapshot{
			FilledOrders: filledOrders, OrderEvents: orderEvents, DecisionReason: s.DecisionReason, ExecutionReason: s.ExecutionReason, Risk: diagnostic,
			Timestamp:      s.Timestamp.UTC(),
			Bar:            bar,
			Indicator:      indicators,
			Signal:         signal,
			Position:       mapPosition(s.Position),
			Account:        mapAccount(s.Account),
			SubmittedOrder: mapOrder(s.SubmittedOrder),
			FilledOrder:    mapOrder(s.FilledOrder),
		})
	}
	if _, err := json.Marshal(response); err != nil {
		return BacktestResultResponse{}, &APIError{
			Code:    "invalid_result",
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
		Equity:      a.Equity,
		Cash:        a.Cash,
		BuyingPower: a.BuyingPower,
	}
}
func mapPosition(p account.Position) Position {
	out := Position{
		Symbol:       p.Symbol,
		Quantity:     p.Quantity,
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
		ID:        o.ID,
		Symbol:    o.Symbol,
		Action:    strings.ToLower(o.Action),
		Quantity:  o.Quantity,
		Status:    strings.ToLower(o.Status),
		CreatedAt: o.CreatedAt.UTC(),
	}
	if out.Status == "filled" {
		out.FilledPrice = number(o.FilledPrice)
	}
	return out
}
