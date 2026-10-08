package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDashboardConfigMatchesServerLimits(t *testing.T) {
	h, _ := httpTestHandler(t, func(context.Context, string, ValidatedRequest) (BacktestResultResponse, error) {
		t.Error("configuration must not start a calculation")
		return BacktestResultResponse{}, nil
	})
	h.options.MaxRangeDays = 90
	h.options.MaxSnapshots = 180
	w := requestHTTP(h, "GET", "/api/config", "", "")
	var got DashboardConfig
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatal(w.Body.String())
	}
	if got.MaxRangeDays != 90 || got.MaxSnapshots != 180 || got.MaxSymbols != 8 || got.EarliestStartDate != "2017-01-01" || got.MarketTimezone != "America/New_York" || got.PercentTotalTolerance != PercentTotalTolerance {
		t.Fatalf("incorrect public limits: %+v", got)
	}
	location, _ := time.LoadLocation(got.MarketTimezone)
	last, err := time.ParseInLocation(time.DateOnly, got.LatestEndDate, location)
	if err != nil || last.AddDate(0, 0, 1).Add(-time.Second).After(time.Now().Add(-15*time.Minute)) {
		t.Fatal("configuration advertises an incomplete end date")
	}
	if strings.Contains(w.Body.String(), "key") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("configuration contains private fields or may be cached")
	}
	w = requestHTTP(h, "POST", "/api/config", "", "")
	if w.Code != 405 || w.Header().Get("Allow") != "GET" {
		t.Fatal("configuration endpoint accepted a mutation")
	}
}

func TestDashboardEarliestStartDateEnforcedByAPI(t *testing.T) {
	r := validRequest()
	r.StartDate, r.EndDate = "2016-12-31", "2017-01-03"
	if _, err := ValidateCreateBacktestRequest(r, testNow()); err == nil {
		t.Fatal("accepted a date before the dashboard's supported history")
	}
	r.StartDate = "2017-01-01"
	if _, err := ValidateCreateBacktestRequest(r, testNow()); err != nil {
		t.Fatal(err)
	}
}
