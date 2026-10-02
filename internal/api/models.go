// Package api defines the public JSON contract independently of engine models.
package api

import "time"

type CreateBacktestRequest struct {
	InitialCapital float64      `json:"initial_capital"`
	Allocations    []Allocation `json:"allocations"`
	StartDate      string       `json:"start_date"`
	EndDate        string       `json:"end_date"`
}
type Allocation struct {
	Symbol  string  `json:"symbol"`
	Percent float64 `json:"percent"`
}
type JobStatus string

const (
	StatusQueued    JobStatus = "queued"
	StatusRunning   JobStatus = "running"
	StatusCompleted JobStatus = "completed"
	StatusFailed    JobStatus = "failed"
)

type CreateBacktestResponse struct {
	ID     string    `json:"id"`
	Status JobStatus `json:"status"`
}
type JobStatusResponse struct {
	ID     string    `json:"id"`
	Status JobStatus `json:"status"`
	Error  *APIError `json:"error"`
}
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

func (e *APIError) Error() string {
	return e.Message
}

type ErrorResponse struct {
	Error *APIError `json:"error"`
}

// Settings are supplied by server configuration, not accepted from the browser.
// Seed zero denotes randomized execution, not a captured reproducible random seed.
type SimulationSettings struct {
	VolatilityWindow  int     `json:"volatility_window"`
	PeriodsPerYear    float64 `json:"periods_per_year"`
	InitialVolatility float64 `json:"initial_volatility"`
	TimeHorizon       float64 `json:"time_horizon"`
	NumPaths          int     `json:"num_paths"`
	NumSteps          int     `json:"num_steps"`
	Seed              int64   `json:"seed"`
}
type DataInfo struct {
	Feed         string  `json:"feed"`
	Timeframe    string  `json:"timeframe"`
	Adjustment   string  `json:"adjustment"`
	RiskFreeRate float64 `json:"risk_free_rate"`
	// The current provider returns a rate without its observation date.
	// Keep that date null until the provider supplies it; do not invent one.
	RiskFreeRateDate *string `json:"risk_free_rate_date"`
}
type BacktestResultResponse struct {
	ID             string                `json:"id"`
	Request        CreateBacktestRequest `json:"request"`
	Settings       SimulationSettings    `json:"settings"`
	Data           DataInfo              `json:"data"`
	FinalAccount   Account               `json:"final_account"`
	FinalPositions map[string]Position   `json:"final_positions"`
	Snapshots      []Snapshot            `json:"snapshots"`
}
type Account struct {
	Equity      float64 `json:"equity"`
	Cash        float64 `json:"cash"`
	BuyingPower float64 `json:"buying_power"`
}
type Position struct {
	Symbol          string   `json:"symbol"`
	Quantity        float64  `json:"quantity"`
	CurrentPrice    float64  `json:"current_price"`
	EntryPrice      *float64 `json:"entry_price"`
	StopLossPrice   *float64 `json:"stop_loss_price"`
	TakeProfitPrice *float64 `json:"take_profit_price"`
}
type Bar struct {
	Symbol    string    `json:"symbol"`
	Open      float64   `json:"open"`
	High      float64   `json:"high"`
	Low       float64   `json:"low"`
	Close     float64   `json:"close"`
	Volume    float64   `json:"volume"`
	VWAP      *float64  `json:"vwap"`
	Timestamp time.Time `json:"timestamp"`
}
type Indicators struct {
	VWAP              *float64 `json:"vwap"`
	StandardDeviation *float64 `json:"standard_deviation"`
	UpperBand         *float64 `json:"upper_band"`
	LowerBand         *float64 `json:"lower_band"`
}

// Signal is an opportunity, not a submitted order. The engine does not retain
// Monte Carlo probabilities in snapshots, so they are not advertised here.
type Signal struct {
	Symbol    string    `json:"symbol"`
	Action    string    `json:"action"`
	Price     *float64  `json:"price"`
	CreatedAt time.Time `json:"created_at"`
}
type Order struct {
	ID          string    `json:"id"`
	Symbol      string    `json:"symbol"`
	Action      string    `json:"action"`
	Quantity    float64   `json:"quantity"`
	FilledPrice *float64  `json:"filled_price"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}
type Snapshot struct {
	Timestamp      time.Time  `json:"timestamp"`
	Bar            Bar        `json:"bar"`
	Indicator      Indicators `json:"indicator"`
	Signal         Signal     `json:"signal"`
	Position       Position   `json:"position"`
	Account        Account    `json:"account"`
	SubmittedOrder *Order     `json:"submitted_order"`
	FilledOrder    *Order     `json:"filled_order"`
}
