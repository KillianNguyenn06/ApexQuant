package api

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"strings"
	"time"

	"apexquant/internal/session"
)

// PercentTotalTolerance matches the engine's 0.000001 fractional tolerance.
const PercentTotalTolerance = 0.0001

var tickerPattern = regexp.MustCompile(`^[A-Z][A-Z0-9.-]{0,9}$`)

type ValidatedRequest struct {
	Request     CreateBacktestRequest
	Allocations []session.PortfolioAllocation
	Start       time.Time
	End         time.Time
}

// DecodeCreateBacktestRequest accepts exactly one JSON object. Handlers will
// enforce HTTP content type and body-size limits before calling this function.
func DecodeCreateBacktestRequest(reader io.Reader) (CreateBacktestRequest, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var request *CreateBacktestRequest
	if err := decoder.Decode(&request); err != nil || request == nil {
		return CreateBacktestRequest{}, &APIError{
			Code: "invalid_json",
			Message: "Expected a backtest JSON object with recognized fields.",
		}
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return CreateBacktestRequest{}, &APIError{
			Code: "invalid_json",
			Message: "Expected exactly one JSON object.",
		}
	}
	return *request, nil
}

// ValidateCreateBacktestRequest is pure: time is supplied by the caller, and
// normalization copies allocations rather than mutating browser input.
// Dates include both requested days, interpreted in America/New_York.
func ValidateCreateBacktestRequest(request CreateBacktestRequest, now time.Time) (ValidatedRequest, error) {
	invalid := func(code, message, field string) (ValidatedRequest, error) {
		return ValidatedRequest{}, &APIError{
			Code: code,
			Message: message,
			Field: field,
		}
	}
	if !finite(request.InitialCapital) || request.InitialCapital <= 0 {
		return invalid("invalid_capital", "Initial capital must be finite and greater than zero.", "initial_capital")
	}
	if len(request.Allocations) < 1 || len(request.Allocations) > 8 {
		return invalid("invalid_allocations", "Select between 1 and 8 symbols.", "allocations")
	}
	normalized := request
	normalized.Allocations = make([]Allocation, len(request.Allocations))
	allocations := make([]session.PortfolioAllocation, len(request.Allocations))
	seen := map[string]bool{}
	total := 0.0
	for i, a := range request.Allocations {
		symbol := strings.ToUpper(strings.TrimSpace(a.Symbol))
		if !tickerPattern.MatchString(symbol) {
			return invalid("invalid_symbol", "Symbol format is invalid.", fmt.Sprintf("allocations[%d].symbol", i))
		}
		if seen[symbol] {
			return invalid("duplicate_symbol", "Symbols must be unique after normalization.", fmt.Sprintf("allocations[%d].symbol", i))
		}
		seen[symbol] = true
		if !finite(a.Percent) || a.Percent <= 0 || a.Percent > 100 {
			return invalid("invalid_allocations", "Each percentage must be greater than zero and no greater than 100.", fmt.Sprintf("allocations[%d].percent", i))
		}
		total += a.Percent
		normalized.Allocations[i] = Allocation{
			Symbol: symbol,
			Percent: a.Percent,
		}
		allocations[i] = session.PortfolioAllocation{
			Symbol: symbol,
			Weight: a.Percent / 100,
		}
	}
	if math.Abs(total-100) > PercentTotalTolerance {
		return invalid("invalid_allocations", "Allocation percentages must total 100.", "allocations")
	}
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		return invalid("internal_error", "Market timezone is unavailable.", "")
	}
	start, err := time.ParseInLocation(time.DateOnly, request.StartDate, location)
	if err != nil {
		return invalid("invalid_dates", "Use a valid YYYY-MM-DD start date.", "start_date")
	}
	last, err := time.ParseInLocation(time.DateOnly, request.EndDate, location)
	if err != nil {
		return invalid("invalid_dates", "Use a valid YYYY-MM-DD end date.", "end_date")
	}
	if !start.Before(last) {
		return invalid("invalid_dates", "Start date must precede end date.", "end_date")
	}
	end := last.AddDate(0, 0, 1).Add(-time.Second)
	// Use a conservative completed-calendar-day boundary, including SIP's delay.
	if now.IsZero() || end.After(now.Add(-15*time.Minute)) {
		return invalid("invalid_dates", "End date must be a completed US market day outside the latest 15 minutes.", "end_date")
	}
	return ValidatedRequest{
		Request: normalized,
		Allocations: allocations,
		Start: start.UTC(),
		End: end.UTC(),
		}, nil
}
func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

// ValidateRequestLimits bounds HTTP work without reducing engine fidelity.
// Calendar days are an upper bound on daily bars, not an assumed trading calendar.
func ValidateRequestLimits(request ValidatedRequest, maxRangeDays, maxSnapshots int) error {
	start, err := time.Parse(time.DateOnly, request.Request.StartDate)
	if err != nil {
		return &APIError{Code: "invalid_dates", Message: "Invalid start date.", Field: "start_date"}
	}
	end, err := time.Parse(time.DateOnly, request.Request.EndDate)
	if err != nil {
		return &APIError{Code: "invalid_dates", Message: "Invalid end date.", Field: "end_date"}
	}
	days := int((end.Unix()-start.Unix())/86400) + 1
	if days < 2 || days > maxRangeDays {
		return &APIError{Code: "workload_limit", Message: fmt.Sprintf("Date range must contain 2 to %d calendar days.", maxRangeDays), Field: "end_date"}
	}
	if len(request.Allocations) < 1 || days > maxSnapshots/len(request.Allocations) {
		return &APIError{Code: "workload_limit", Message: "Requested symbols and dates exceed the configured snapshot budget.", Field: "allocations"}
	}
	return nil
}
