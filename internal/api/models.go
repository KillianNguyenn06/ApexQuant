// Package api defines the public JSON contract independently of engine models.
package api

import (
	"apexquant/internal/simulation"
	"time"
)

type CreateBacktestRequest struct {
	HoldingDays           int          `json:"holding_days,omitempty"`
	ProbabilityModel      string       `json:"probability_model,omitempty"`
	ReversionHalfLifeDays float64      `json:"reversion_half_life_days,omitempty"`
	VWAPWindow            int          `json:"vwap_window,omitempty"`
	Timeframe             string       `json:"timeframe,omitempty"`
	InitialCapital        float64      `json:"initial_capital"`
	Allocations           []Allocation `json:"allocations"`
	StartDate             string       `json:"start_date"`
	EndDate               string       `json:"end_date"`
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
	Provider string `json:"provider,omitempty"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Field    string `json:"field,omitempty"`
}

func (e *APIError) Error() string {
	return e.Message
}

type ErrorResponse struct {
	Error *APIError `json:"error"`
}

// Model settings are supplied by the server. The daily indicator window is user configurable.
// Seed zero denotes randomized execution, not a captured reproducible random seed.
type SimulationSettings struct {
	ReversionEstimates    map[string]simulation.ReversionEstimate `json:"reversion_estimates,omitempty"`
	HoldingDays           int                                     `json:"holding_days"`
	ProbabilityModel      string                                  `json:"probability_model"`
	ReversionHalfLifeDays float64                                 `json:"reversion_half_life_days,omitempty"`
	DailyVWAPWindow       int                                     `json:"daily_vwap_window,omitempty"`
	VolatilityWindow      int                                     `json:"volatility_window"`
	PeriodsPerYear        float64                                 `json:"periods_per_year"`
	InitialVolatility     float64                                 `json:"initial_volatility"`
	TimeHorizon           float64                                 `json:"time_horizon"`
	NumPaths              int                                     `json:"num_paths"`
	NumSteps              int                                     `json:"num_steps"`
	Seed                  int64                                   `json:"seed"`
}
type DataInfo struct {
	RatePolicy   string  `json:"rate_policy,omitempty"`
	Feed         string  `json:"feed"`
	Timeframe    string  `json:"timeframe"`
	Adjustment   string  `json:"adjustment"`
	RiskFreeRate float64 `json:"risk_free_rate"`
	// Initial historical observation date; legacy fixed-rate providers may omit it.
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
	EMA               *float64 `json:"ema,omitempty"`
	EMASlope          *float64 `json:"ema_slope,omitempty"`
	EMAReady          bool     `json:"ema_ready,omitempty"`
	VWAP              *float64 `json:"vwap"`
	StandardDeviation *float64 `json:"standard_deviation"`
	UpperBand         *float64 `json:"upper_band"`
	LowerBand         *float64 `json:"lower_band"`
}

// Signal is an opportunity, not a submitted order. Model probabilities and
// sizing diagnostics, when evaluated, are retained separately in Snapshot.Risk.
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
	FilledOrders    []Order       `json:"filled_orders,omitempty"`
	OrderEvents     []Order       `json:"order_events,omitempty"`
	DecisionReason  string        `json:"decision_reason,omitempty"`
	ExecutionReason string        `json:"execution_reason,omitempty"`
	Risk            *RiskDecision `json:"risk,omitempty"`
	Timestamp       time.Time     `json:"timestamp"`
	Bar             Bar           `json:"bar"`
	Indicator       Indicators    `json:"indicator"`
	Signal          Signal        `json:"signal"`
	Position        Position      `json:"position"`
	Account         Account       `json:"account"`
	SubmittedOrder  *Order        `json:"submitted_order"`
	FilledOrder     *Order        `json:"filled_order"`
}

type RiskDecision struct {
	RiskFreeRate       float64 `json:"risk_free_rate"`
	Model              string  `json:"model"`
	TimeoutMeanReturn  float64 `json:"timeout_mean_return"`
	WinProbability     float64 `json:"win_probability"`
	LossProbability    float64 `json:"loss_probability"`
	TimeoutProbability float64 `json:"timeout_probability"`
	KellyFraction      float64 `json:"kelly_fraction"`
	Quantity           float64 `json:"quantity"`
}
