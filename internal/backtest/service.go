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
	// Service.Run supplies the requested end instant in America/New_York.
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
	InitialCapital float64
	Allocations    []session.PortfolioAllocation
	Start, End     time.Time
}
type RunOutput struct {
	Config BacktestConfig
	Result BacktestResult
}

// ServiceError deliberately contains no provider error text or credentials.
type ServiceError struct {
	Code string
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
	c := BacktestConfig{
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
	for _, a := range allocations {
		// A bounded calendar buffer accommodates ordinary weekends and holidays.
		fetchStart := request.Start.AddDate(0, 0, -(2*s.settings.VolatilityWindow + 14))
		bars, err := s.provider.FetchBars(ctx, a.Symbol, fetchStart, request.End, s.credentials.AlpacaKey, s.credentials.AlpacaSecret)
		if err != nil {
			if ctx.Err() != nil {
				return fail("canceled")
			}
			return fail("provider_error")
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
		c.VolatilityHistory[a.Symbol] = bars[max(0, split-s.settings.VolatilityWindow):split]
	}

	// FRED accepts a calendar date, while Alpaca accepts UTC timestamp bounds.
	// Select the requested US market date without changing the end instant.
	// The constant-rate model remains; historical curves remain future work.
	rate, err := s.provider.FetchRiskFreeRate(ctx, s.credentials.FREDKey, request.End.In(s.marketLocation))
	if err != nil {
		if ctx.Err() != nil {
			return fail("canceled")
		}
		return fail("provider_error")
	}
	c.MonteCarloInput.RiskFreeRate = rate
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
