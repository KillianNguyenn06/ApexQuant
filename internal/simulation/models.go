package simulation

import (
	"math"
	"math/rand"
	"sync"
	"time"

	"apexquant/internal/algorithm"
)

type MonteCarlo struct {
	UnderlyingPrice float64 `json:"underlying_price"` // S0
	StrikePrice     float64 `json:"strike_price"`     // K
	StopLossPrice   float64 `json:"stop_loss_price"`
	Volatility      float64 `json:"volatility"`     // sigma
	RiskFreeRate    float64 `json:"risk_free_rate"` // r
	TimeHorizon     float64 `json:"time_horizon"`   // T
	NumPaths        float64 `json:"num_paths"`      // N e.g: 10000
	NumSteps        float64 `json:"num_steps"`      // 252 for Standard, 100 for shorter interval
}

type MonteCarloResult struct {
	WinProbability     float64 `json:"win_probability"`
	LossProbability    float64 `json:"loss_probability"`
	ExpectedReturn     float64 `json:"expected_return"`
	TimeoutProbability float64 `json:"timeout_probability"`
}

func GBMMonteCarlo(S0, r, T, sigma float64, steps, pathCount int, indicator algorithm.Indicator) (float64, float64, float64) {

	wins := 0.0
	losses := 0.0
	timeOut := 0.0
	dt := T / float64(steps)
	drift := (r - 0.5*sigma*sigma) * dt
	volatility := sigma * math.Sqrt(dt)

	var wg sync.WaitGroup

	for i := 0; i < pathCount; i++ {
		wg.Add(1)
		go func(pathIndex int) {
			defer wg.Done()
			st := S0
			localRand := rand.New(rand.NewSource(time.Now().UnixNano() + int64(pathIndex))) // Create a new random source for each goroutine
			for j := 0; j < steps; j++ {

				z := localRand.NormFloat64()           // Generate a random number from a standard normal distribution
				st = st * math.Exp(drift+volatility*z) // Calculate the next price using the GBM formula

				if st >= indicator.UpperBand {
					wins++
				} else if st <= indicator.LowerBand {
					losses++
				} else {
					timeOut++
				}
			}

		}(i)
	}
	wg.Wait()

	return wins, losses, timeOut
}
