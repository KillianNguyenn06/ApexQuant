package simulation

type MonteCarlo struct {
	UnderlyingPrice float64 `json:"underlying_price"`
	StrikePrice     float64 `json:"strike_price"`
	StopLossPrice   float64 `json:"stop_loss_price"`
	Volatility      float64 `json:"volatility"`
	TimeHorizon     float64 `json:"time_horizon"`
	NumPaths        float64 `json:"num_paths"`
	NumSteps        float64 `json:"num_steps"`
}

type MonteCarloResult struct {
	WinProbability  float64 `json:"win_probability"`
	LossProbability float64 `json:"loss_probability"`
	ExpectedReturn  float64 `json:"expected_return"`
}
