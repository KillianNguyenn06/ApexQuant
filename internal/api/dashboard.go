package api

import (
	"net/http"
	"time"
)

// DashboardConfig describes public input limits; provider credentials stay server-side.
type DashboardConfig struct {
	EarliestStartDate     string  `json:"earliest_start_date"`
	LatestEndDate         string  `json:"latest_end_date"`
	MaxRangeDays          int     `json:"max_range_days"`
	MaxSnapshots          int     `json:"max_snapshots"`
	MaxSymbols            int     `json:"max_symbols"`
	PercentTotalTolerance float64 `json:"percent_total_tolerance"`
	MarketTimezone        string  `json:"market_timezone"`
}

func (h *Handler) dashboardConfig(w http.ResponseWriter) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		writeError(w, 500, "internal_error", "Market timezone is unavailable.")
		return
	}
	local := time.Now().Add(-15 * time.Minute).In(location)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	writeJSON(w, 200, DashboardConfig{
		EarliestStartDate:     EarliestStartDate,
		LatestEndDate:         today.AddDate(0, 0, -1).Format(time.DateOnly),
		MaxRangeDays:          h.options.MaxRangeDays,
		MaxSnapshots:          h.options.MaxSnapshots,
		MaxSymbols:            MaxSymbols,
		PercentTotalTolerance: PercentTotalTolerance,
		MarketTimezone:        "America/New_York",
	})
}
