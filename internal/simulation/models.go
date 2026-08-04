package simulation

import (
	"math"
	"math/rand"
	"sync"
	"time"

	"apexquant/internal/account"
)

type MonteCarlo struct {
	UnderlyingPrice float64 `json:"underlying_price"` // S0
	TargetPrice     float64 `json:"target_price"`     // K
	StopLossPrice   float64 `json:"stop_loss_price"`
	Volatility      float64 `json:"volatility"`     // sigma
	RiskFreeRate    float64 `json:"risk_free_rate"` // r
	TimeHorizon     float64 `json:"time_horizon"`   // T
	NumPaths        int     `json:"num_paths"`      // N e.g: 10000
	NumSteps        int     `json:"num_steps"`      // 252 for Standard, 100 for shorter interval
}

type MonteCarloResult struct {
	WinProbability     float64 `json:"win_probability"`
	LossProbability    float64 `json:"loss_probability"`
	ExpectedReturn     float64 `json:"expected_return"`
	TimeoutProbability float64 `json:"timeout_probability"`
}

func GBMModel(input MonteCarlo, position account.Position, result *MonteCarloResult) (float64, float64, float64) {

	wins := 0.0
	losses := 0.0
	timeOut := 0.0
	dt := input.TimeHorizon / float64(input.NumSteps)
	drift := (input.RiskFreeRate - 0.5*math.Pow(input.Volatility, 2)) * dt
	volatility := input.Volatility * math.Sqrt(dt)

	var wg sync.WaitGroup
	var mu sync.Mutex

	for i := 0; i < input.NumPaths; i++ {
		wg.Add(1)
		go func(pathIndex int) {
			defer wg.Done()
			outcome := "timeout"
			st := input.UnderlyingPrice
			localRand := rand.New(rand.NewSource(time.Now().UnixNano() + int64(pathIndex))) // Create a new random source for each goroutine
			for j := 0; j < input.NumSteps; j++ {
				z := localRand.NormFloat64()           // Generate a random number from a standard normal distribution
				st = st * math.Exp(drift+volatility*z) // Calculate the next price using the GBM formula
				if st >= position.TakeProfitPrice {
					outcome = "win"
					break
				} else if st <= position.StopLossPrice {
					outcome = "loss"
					break
				}
			}
			mu.Lock()
			defer mu.Unlock()
			switch outcome {
			case "win":
				wins++
			case "loss":
				losses++
			default:
				timeOut++
			}
		}(i)
	}
	wg.Wait()
	result.WinProbability = wins / float64(input.NumPaths)
	result.LossProbability = losses / float64(input.NumPaths)
	result.TimeoutProbability = timeOut / float64(input.NumPaths)

	return result.WinProbability, result.LossProbability, result.TimeoutProbability
}
