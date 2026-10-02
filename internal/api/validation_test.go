package api

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func validRequest() CreateBacktestRequest {
	return CreateBacktestRequest{InitialCapital: 50000, Allocations: []Allocation{{Symbol: " aapl ", Percent: 50}, {Symbol: "MSFT", Percent: 50}}, StartDate: "2025-09-05", EndDate: "2026-09-04"}
}
func testNow() time.Time { return time.Date(2026, 9, 5, 16, 0, 0, 0, time.UTC) }
func TestNormalizeAndConvert(t *testing.T) {
	r := validRequest()
	v, err := ValidateCreateBacktestRequest(r, testNow())
	if err != nil {
		t.Fatal(err)
	}
	if v.Request.Allocations[0].Symbol != "AAPL" || v.Allocations[0].Weight != .5 || r.Allocations[0].Symbol != " aapl " {
		t.Fatal("conversion mutated input or produced wrong weights")
	}
	if v.Start.Format(time.RFC3339) != "2025-09-05T04:00:00Z" || v.End.Format(time.RFC3339) != "2026-09-05T03:59:59Z" {
		t.Fatalf("incorrect inclusive range %s %s", v.Start, v.End)
	}
	v.Request.Allocations[0].Percent = 1
	if r.Allocations[0].Percent != 50 {
		t.Fatal("aliased allocations")
	}
}
func TestInvalidRequests(t *testing.T) {
	cases := []struct {
		name, field string
		change      func(*CreateBacktestRequest)
	}{
		{"zero capital", "initial_capital", func(r *CreateBacktestRequest) { r.InitialCapital = 0 }},
		{"nan capital", "initial_capital", func(r *CreateBacktestRequest) { r.InitialCapital = math.NaN() }},
		{"infinite capital", "initial_capital", func(r *CreateBacktestRequest) { r.InitialCapital = math.Inf(1) }},
		{"no allocations", "allocations", func(r *CreateBacktestRequest) { r.Allocations = nil }},
		{"nine symbols", "allocations", func(r *CreateBacktestRequest) { r.Allocations = make([]Allocation, 9) }},
		{"duplicate", "allocations[1].symbol", func(r *CreateBacktestRequest) { r.Allocations[1].Symbol = "AAPL" }},
		{"bad symbol", "allocations[0].symbol", func(r *CreateBacktestRequest) { r.Allocations[0].Symbol = "AAPL!" }},
		{"zero percent", "allocations[0].percent", func(r *CreateBacktestRequest) { r.Allocations[0].Percent = 0 }},
		{"nan percent", "allocations[0].percent", func(r *CreateBacktestRequest) { r.Allocations[0].Percent = math.NaN() }},
		{"wrong total", "allocations", func(r *CreateBacktestRequest) { r.Allocations[0].Percent = 49 }},
		{"impossible date", "start_date", func(r *CreateBacktestRequest) { r.StartDate = "2025-02-30" }},
		{"timestamp instead of date", "end_date", func(r *CreateBacktestRequest) { r.EndDate = "2026-09-04T00:00:00Z" }},
		{"same day", "end_date", func(r *CreateBacktestRequest) { r.StartDate = r.EndDate }},
		{"reversed dates", "end_date", func(r *CreateBacktestRequest) { r.StartDate = "2026-09-05" }},
		{"current day", "end_date", func(r *CreateBacktestRequest) { r.EndDate = "2026-09-05" }},
		{"future day", "end_date", func(r *CreateBacktestRequest) { r.EndDate = "2027-09-05" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := validRequest()
			tc.change(&r)
			_, err := ValidateCreateBacktestRequest(r, testNow())
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Field != tc.field {
				t.Fatalf("error %v, expected field %s", err, tc.field)
			}
		})
	}
}
func TestPercentageTolerance(t *testing.T) {
	for _, tc := range []struct {
		delta float64
		ok    bool
	}{{.00005, true}, {.0002, false}} {
		r := validRequest()
		r.Allocations[0].Percent += tc.delta
		_, err := ValidateCreateBacktestRequest(r, testNow())
		if (err == nil) != tc.ok {
			t.Fatalf("delta=%g error=%v", tc.delta, err)
		}
	}
}
func TestDateBoundaries(t *testing.T) {
	for _, tc := range []struct{ start, end, wantStart, wantEnd string }{
		{"2026-03-07", "2026-03-09", "2026-03-07T05:00:00Z", "2026-03-10T03:59:59Z"},
		{"2025-11-01", "2025-11-03", "2025-11-01T04:00:00Z", "2025-11-04T04:59:59Z"},
	} {
		r := validRequest()
		r.StartDate = tc.start
		r.EndDate = tc.end
		v, err := ValidateCreateBacktestRequest(r, testNow())
		if err != nil {
			t.Fatal(err)
		}
		if v.Start.Format(time.RFC3339) != tc.wantStart || v.End.Format(time.RFC3339) != tc.wantEnd {
			t.Fatal("DST boundaries incorrect")
		}
	}
	r := validRequest()
	now := time.Date(2026, 9, 5, 4, 5, 0, 0, time.UTC)
	if _, err := ValidateCreateBacktestRequest(r, now); err == nil {
		t.Fatal("accepted data within SIP delay")
	}
}
func TestDecodeContract(t *testing.T) {
	for _, input := range []string{`null`, `[]`, `{`, `{} {}`, `{"api_key":"secret"}`, `{"initial_capital":"50000"}`, `{} garbage`} {
		if _, err := DecodeCreateBacktestRequest(strings.NewReader(input)); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	r, err := DecodeCreateBacktestRequest(strings.NewReader(`{"initial_capital":50000,"allocations":[{"symbol":"AAPL","percent":100}],"start_date":"2025-09-05","end_date":"2026-09-04"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateCreateBacktestRequest(r, testNow()); err != nil {
		t.Fatal(err)
	}
}
