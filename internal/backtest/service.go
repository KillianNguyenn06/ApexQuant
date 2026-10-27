package backtest

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"apexquant/internal/account"
	"apexquant/internal/marketdata"
	"apexquant/internal/session"
	"apexquant/internal/simulation"
)

// DataProvider lets terminal and HTTP callers use the same orchestration without
// making the engine depend on a provider, network transport, or API DTOs.
type DataProvider interface {
	FetchBars(context.Context, string, time.Time, time.Time, string, string) ([]marketdata.BarTick, error)
	// FetchRiskFreeRate uses the calendar date in the supplied time's location.
	// Legacy providers are frozen strictly before the run starts.
	FetchRiskFreeRate(context.Context, string, time.Time) (float64, error)
}
type Credentials struct {
	AlpacaKey, AlpacaSecret, FREDKey string
}
type ServiceSettings struct {
	MonteCarlo       simulation.MonteCarlo
	VolatilityWindow int
	PeriodsPerYear   float64
}

func DefaultServiceSettings() ServiceSettings {
	return ServiceSettings{
		MonteCarlo: simulation.MonteCarlo{
			Volatility:  .20,
			TimeHorizon: 5.0 / 252,
			NumPaths:    100000,
			NumSteps:    252,
		},
		VolatilityWindow: 20,
		PeriodsPerYear:   252,
	}
}

type RunRequest struct {
	HoldingDays           int
	ProbabilityModel      string
	ReversionHalfLifeDays float64
	DailyVWAPWindow       int
	Timeframe             string
	InitialCapital        float64
	Allocations           []session.PortfolioAllocation
	Start, End            time.Time
}
type RunOutput struct {
	Config BacktestConfig
	Result BacktestResult
}

// ServiceError deliberately contains no provider error text or credentials.
type ServiceError struct {
	Provider string
	Code     string
}

func (e *ServiceError) Error() string {
	return e.Code
}

type Service struct {
	provider       DataProvider
	credentials    Credentials
	settings       ServiceSettings
	marketLocation *time.Location
}

func NewService(provider DataProvider, credentials Credentials, settings ServiceSettings) (*Service, error) {
	if provider == nil {
		return nil, fmt.Errorf("data provider is required")
	}
	m := settings.MonteCarlo
	for _, v := range []float64{m.Volatility, m.RiskFreeRate, m.TimeHorizon, settings.PeriodsPerYear} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("simulation settings must be finite")
		}
	}
	if m.NumPaths < 1 || m.NumSteps < 1 || m.TimeHorizon <= 0 || m.Volatility < 0 || settings.VolatilityWindow < 3 || settings.PeriodsPerYear <= 0 {
		return nil, fmt.Errorf("invalid simulation settings")
	}
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		return nil, fmt.Errorf("market timezone is unavailable: %w", err)
	}
	return &Service{
		provider:       provider,
		credentials:    credentials,
		settings:       settings,
		marketLocation: location,
	}, nil
}
func (s *Service) Run(ctx context.Context, request RunRequest) (RunOutput, error) {
	fail := func(code string) (RunOutput, error) {
		return RunOutput{}, &ServiceError{Code: code}
	}
	providerFailure := func(provider string) (RunOutput, error) {
		return RunOutput{}, &ServiceError{Code: "provider_error", Provider: provider}
	}
	if err := ctx.Err(); err != nil {
		return fail("canceled")
	}
	if math.IsNaN(request.InitialCapital) || math.IsInf(request.InitialCapital, 0) || request.InitialCapital <= 0 || request.Start.IsZero() || !request.Start.Before(request.End) {
		return fail("invalid_request")
	}
	tickers := make([]string, len(request.Allocations))
	weights := make([]float64, len(tickers))
	seen := map[string]bool{}
	for i, a := range request.Allocations {
		tickers[i] = strings.ToUpper(strings.TrimSpace(a.Symbol))
		weights[i] = a.Weight
		if seen[tickers[i]] {
			return fail("invalid_request")
		}
		seen[tickers[i]] = true
	}
	var allocations []session.PortfolioAllocation
	if err := session.PortfolioAllocate(tickers, weights, &allocations); err != nil {
		return fail("invalid_request")
	}
	if request.Timeframe == "1Min" && !MinuteExecutionEnabled {
		return fail("minute_execution_disabled")
	}
	if request.Timeframe != "" && request.Timeframe != "1Day" && request.Timeframe != "1Min" {
		return fail("invalid_request")
	}
	if request.DailyVWAPWindow != 0 && (request.Timeframe == "1Min" || request.DailyVWAPWindow < 2 || request.DailyVWAPWindow > 252) {
		return fail("invalid_request")
	}
	if request.HoldingDays < 0 || request.HoldingDays > 252 || (request.ProbabilityModel != "" && request.ProbabilityModel != "gbm" && request.ProbabilityModel != "vwap_reversion" && request.ProbabilityModel != "vwap_reversion_auto") || math.IsNaN(request.ReversionHalfLifeDays) || math.IsInf(request.ReversionHalfLifeDays, 0) || request.ReversionHalfLifeDays < 0 || request.ReversionHalfLifeDays > 252 || (request.ProbabilityModel != "vwap_reversion" && request.ReversionHalfLifeDays != 0) {
		return fail("invalid_request")
	}
	c := BacktestConfig{
		HoldingDays:         request.HoldingDays,
		DailyVWAPWindow:     request.DailyVWAPWindow,
		Timeframe:           request.Timeframe,
		DailyVolatilityBars: make(map[string][]marketdata.BarTick),
		InitialAccount: account.Account{
			Cash:        request.InitialCapital,
			Equity:      request.InitialCapital,
			BuyingPower: request.InitialCapital,
		},
		Allocations:       allocations,
		BarsBySymbol:      make(map[string][]marketdata.BarTick),
		VolatilityHistory: make(map[string][]marketdata.BarTick),
		MonteCarloInput:   s.settings.MonteCarlo,
		VolatilityWindow:  s.settings.VolatilityWindow,
		PeriodsPerYear:    s.settings.PeriodsPerYear,
	}
	c.HoldingDays = HoldingPeriod(c)
	c.MonteCarloInput.HoldingDays = c.HoldingDays
	c.MonteCarloInput.TimeHorizon = float64(c.HoldingDays) / s.settings.PeriodsPerYear
	c.MonteCarloInput.Model = request.ProbabilityModel
	if c.MonteCarloInput.Model == "" {
		c.MonteCarloInput.Model = "gbm"
	}
	c.MonteCarloInput.ReversionHalfLifeDays = request.ReversionHalfLifeDays
	if c.MonteCarloInput.Model == "vwap_reversion" && c.MonteCarloInput.ReversionHalfLifeDays == 0 {
		c.MonteCarloInput.ReversionHalfLifeDays = 5
	}
	for _, a := range allocations {
		if request.Timeframe == "1Min" {
			provider, ok := s.provider.(interface {
				FetchBarsTimeframe(context.Context, string, time.Time, time.Time, string, string, string) ([]marketdata.BarTick, error)
			})
			if !ok {
				return fail("unsupported_timeframe")
			}
			daily, err := s.provider.FetchBars(ctx, a.Symbol, request.Start.AddDate(0, 0, -(2*s.settings.VolatilityWindow+14)), request.End, s.credentials.AlpacaKey, s.credentials.AlpacaSecret)
			if err != nil {
				return providerFailure("alpaca")
			}
			if marketdata.ValidateBars(a.Symbol, daily) != nil {
				return fail("invalid_data")
			}
			c.DailyVolatilityBars[a.Symbol] = daily
			bars, err := provider.FetchBarsTimeframe(ctx, a.Symbol, request.Start, request.End, s.credentials.AlpacaKey, s.credentials.AlpacaSecret, "1Min")
			if err != nil {
				return providerFailure("alpaca")
			}
			if marketdata.ValidateBarsTimeframe(a.Symbol, bars, "1Min") != nil {
				return fail("invalid_data")
			}
			for _, bar := range bars {
				if bar.Timestamp.Before(request.Start) || bar.Timestamp.Add(time.Minute).After(request.End) {
					return fail("invalid_data")
				}
				local := bar.Timestamp.In(s.marketLocation)
				minute := local.Hour()*60 + local.Minute()
				if minute >= 570 && minute < 960 {
					c.BarsBySymbol[a.Symbol] = append(c.BarsBySymbol[a.Symbol], bar)
				}
			}
			continue
		}
		// A bounded calendar buffer accommodates ordinary weekends and holidays.
		preparation := s.settings.VolatilityWindow
		if request.ProbabilityModel == "vwap_reversion_auto" {
			preparation = 253 + DailyVWAPWindowSize(c)
		}
		fetchStart := request.Start.AddDate(0, 0, -(2*preparation + 14))
		bars, err := s.provider.FetchBars(ctx, a.Symbol, fetchStart, request.End, s.credentials.AlpacaKey, s.credentials.AlpacaSecret)
		if err != nil {
			if ctx.Err() != nil {
				return fail("canceled")
			}
			return providerFailure("alpaca")
		}
		if err := marketdata.ValidateBars(a.Symbol, bars); err != nil {
			return fail("invalid_data")
		}
		split := 0
		for _, bar := range bars {
			if bar.Timestamp.Before(fetchStart) || bar.Timestamp.After(request.End) {
				return fail("invalid_data")
			}
			if bar.Timestamp.Before(request.Start) {
				split++
			}
		}
		c.BarsBySymbol[a.Symbol] = bars[split:]
		c.VolatilityHistory[a.Symbol] = bars[max(0, split-preparation):split]
	}

	if c.Timeframe == "1Min" {
		reference := c.BarsBySymbol[allocations[0].Symbol]
		for _, a := range allocations[1:] {
			bars := c.BarsBySymbol[a.Symbol]
			if len(bars) != len(reference) {
				return fail("unaligned_minute_data")
			}
			for i, b := range bars {
				if !b.Timestamp.Equal(reference[i].Timestamp) {
					return fail("unaligned_minute_data")
				}
			}
		}
	}
	if err := prepareReversion(&c); err != nil {
		return fail("unreliable_reversion")
	}
	// Fetch vintages once, then select rates locally at each decision date.
	if provider, ok := s.provider.(interface {
		FetchRiskFreeRates(context.Context, string, time.Time, time.Time) ([]marketdata.RateObservation, error)
	}); ok {
		history, err := provider.FetchRiskFreeRates(ctx, s.credentials.FREDKey, request.Start.In(s.marketLocation).AddDate(0, 0, -45), request.End.In(s.marketLocation))
		if err != nil {
			if ctx.Err() != nil {
				return fail("canceled")
			}
			return providerFailure("fred")
		}
		c.RiskFreeRates = history
		c.RatePolicy = "historical_vintages_prior_day"
		first, ok := marketdata.RateAt(history, request.Start.In(s.marketLocation).Format(time.DateOnly))
		if !ok {
			return fail("invalid_data")
		}
		c.MonteCarloInput.RiskFreeRate = first.Rate
		c.InitialRateDate = first.Date
	} else {
		// Explicit compatibility for providers that only expose a single dated rate.
		rate, err := s.provider.FetchRiskFreeRate(ctx, s.credentials.FREDKey, request.Start.In(s.marketLocation).AddDate(0, 0, -1))
		if err != nil {
			if ctx.Err() != nil {
				return fail("canceled")
			}
			return providerFailure("fred")
		}
		c.MonteCarloInput.RiskFreeRate = rate
		c.RatePolicy = "fixed_before_start"
	}

	if ctx.Err() != nil {
		return fail("canceled")
	}
	result, err := RunBacktest(c)
	if err != nil {
		return fail("invalid_data")
	}
	// RunBacktest is synchronous and cannot yet be interrupted mid-calculation.
	if ctx.Err() != nil {
		return fail("canceled")
	}
	return RunOutput{
		Config: c,
		Result: result,
	}, nil
}
