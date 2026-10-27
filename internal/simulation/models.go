package simulation

import (
	"fmt"
	"math"
	"math/rand"
	"runtime"
	"sync"
	"time"

	"apexquant/internal/account"
	"apexquant/internal/algorithm"
	"apexquant/internal/marketdata"
)

type MonteCarlo struct {
	AutoLogDrift          float64 `json:"auto_log_drift,omitempty"`
	HoldingDays           int     `json:"holding_days,omitempty"`
	Model                 string  `json:"model,omitempty"`
	ReversionHalfLifeDays float64 `json:"reversion_half_life_days,omitempty"`
	Volatility            float64 `json:"volatility"`     // sigma
	RiskFreeRate          float64 `json:"risk_free_rate"` // r
	TimeHorizon           float64 `json:"time_horizon"`   // T
	NumPaths              int     `json:"num_paths"`      // N e.g: 10000
	NumSteps              int     `json:"num_steps"`      // 252 for Standard, 100 for shorter interval
	Seed                  int64   `json:"seed"`
}

type MonteCarloResult struct {
	WinProbability  float64 `json:"win_probability"`
	LossProbability float64 `json:"loss_probability"`
	// Payoffs are per unit of planned stop risk, in stable path-index order.
	Payoffs            []float64 `json:"-"`
	TimeoutMeanReturn  float64   `json:"timeout_mean_return"`
	TimeoutProbability float64   `json:"timeout_probability"`
}

// =================================================
// Geometric Brownian Motion (GBM) Model
// =================================================
func GBMModel(input MonteCarlo, position account.Position, result *MonteCarloResult, signal algorithm.Signal) (float64, float64, float64) {

	*result = MonteCarloResult{}
	if input.NumPaths <= 0 || input.NumSteps <= 0 || input.TimeHorizon <= 0 || signal.Price <= 0 || position.StopLossPrice <= 0 || position.StopLossPrice >= signal.Price || position.TakeProfitPrice <= signal.Price {
		return 0, 0, 0
	}
	days := input.HoldingDays
	if days <= 0 {
		days = input.NumSteps
	} // Generic callers retain stepwise barriers over their explicit horizon.
	stepsPerDay := max(1, (input.NumSteps+days-1)/days)
	dt := input.TimeHorizon / float64(days*stepsPerDay)
	drift := (input.RiskFreeRate - .5*input.Volatility*input.Volatility) * dt
	diffusion := input.Volatility * math.Sqrt(dt)
	reversion := (input.Model == "vwap_reversion" || input.Model == "vwap_reversion_auto") && signal.VWAP > 0 && input.ReversionHalfLifeDays > 0
	logVWAP, decay, shift, reversionDiffusion := 0.0, 0.0, 0.0, 0.0
	if reversion {
		k := math.Ln2 * float64(days) / (input.TimeHorizon * input.ReversionHalfLifeDays)
		decay = math.Exp(-k * dt)
		logVWAP = math.Log(signal.VWAP)
		logDrift := input.RiskFreeRate - .5*input.Volatility*input.Volatility
		if input.Model == "vwap_reversion_auto" {
			logDrift = input.AutoLogDrift
		}
		shift = logDrift * (1 - decay) / k
		reversionDiffusion = input.Volatility * math.Sqrt((1-decay*decay)/(2*k))
	}
	seed := input.Seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	result.Payoffs = make([]float64, input.NumPaths)
	outcomes := make([]byte, input.NumPaths)
	endings := make([]float64, input.NumPaths)
	var wg sync.WaitGroup
	workers := min(runtime.GOMAXPROCS(0), input.NumPaths)
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(1))
			for i := worker; i < input.NumPaths; i += workers {
				rng.Seed(seed + int64(i))
				st := signal.Price
				exit := st
				for day := 0; day < days; day++ {
					open, low, high := st, st, st
					for step := 0; step < stepsPerDay; step++ {
						z := rng.NormFloat64()
						if reversion {
							x := logVWAP + (math.Log(st)-logVWAP)*decay + shift
							st = math.Exp(x + reversionDiffusion*z)
						} else {
							st *= math.Exp(drift + diffusion*z)
						}
						low = min(low, st)
						high = max(high, st)
					}
					// Use the same daily OHLC convention as execution: opening gaps first,
					// then stop-first if both boundaries touch. A deadline exits at close.
					switch {
					case open <= position.StopLossPrice:
						exit = open
						outcomes[i] = 2
					case open >= position.TakeProfitPrice:
						exit = open
						outcomes[i] = 1
					case low <= position.StopLossPrice:
						exit = position.StopLossPrice
						outcomes[i] = 2
					case high >= position.TakeProfitPrice:
						exit = position.TakeProfitPrice
						outcomes[i] = 1
					default:
						exit = st
					}
					if outcomes[i] != 0 {
						break
					}
				}
				result.Payoffs[i] = (exit - signal.Price) / (signal.Price - position.StopLossPrice)
				endings[i] = exit/signal.Price - 1
			}
		}(worker)
	}
	wg.Wait()
	var wins, losses, timeouts int
	for i, outcome := range outcomes {
		switch outcome {
		case 1:
			wins++
		case 2:
			losses++
		default:
			timeouts++
			result.TimeoutMeanReturn += endings[i]
		}
	}
	n := float64(input.NumPaths)
	result.WinProbability = float64(wins) / n
	result.LossProbability = float64(losses) / n
	result.TimeoutProbability = float64(timeouts) / n
	if timeouts > 0 {
		result.TimeoutMeanReturn /= float64(timeouts)
	}
	return result.WinProbability, result.LossProbability, result.TimeoutProbability
}

// =================================================
// Calculate Annual Volatility
// =================================================
func AnnualizedVolatility(bars []marketdata.BarTick, periodPerYear float64) (float64, error) {
	if len(bars) < 3 {
		return 0, fmt.Errorf("\n\tError: At least 3 bars are required.\n")
	}

	returns := make([]float64, 0, len(bars)-1)

	for i := 1; i < len(bars); i++ {
		if bars[i-1].Close <= 0 || bars[i].Close <= 0 {
			return 0, fmt.Errorf("\n\tError: Close price must be positive.\n")
		}
		logReturn := math.Log(bars[i].Close / bars[i-1].Close)
		returns = append(returns, logReturn)
	}

	var mean float64
	for _, value := range returns {
		mean += value
	}
	mean /= float64(len(returns))

	var variance float64
	for _, value := range returns {
		difference := value - mean
		variance += math.Pow(difference, 2)
	}
	variance /= float64(len(returns) - 1)
	return math.Sqrt(variance) * math.Sqrt(periodPerYear), nil
}
