package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"apexquant/internal/backtest"
	"apexquant/internal/marketdata"
	"apexquant/internal/session"
)

type calendarTransport func(*http.Request) (*http.Response, error)

func (f calendarTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Exercise the real request-date conversion, service, and outbound provider
// request together, using only a fake HTTP transport.
func TestFREDMarketCalendarBoundary(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, endDate := range []string{"2026-09-03", "2026-01-08"} {
		last, err := time.ParseInLocation(time.DateOnly, endDate, location)
		if err != nil {
			t.Fatal(err)
		}
		for _, source := range []string{"API", "CLI"} {
			for _, futureObservation := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/future=%t", endDate, source, futureObservation), func(t *testing.T) {
					now := last.AddDate(0, 0, 1).Add(12 * time.Hour)
					request := backtest.RunRequest{InitialCapital: 50000, Allocations: []session.PortfolioAllocation{{Symbol: "AAPL", Weight: 1}}}
					if source == "API" {
						validated, err := ValidateCreateBacktestRequest(CreateBacktestRequest{
							InitialCapital: 50000,
							Allocations:    []Allocation{{Symbol: "AAPL", Percent: 100}},
							StartDate:      last.AddDate(0, 0, -2).Format(time.DateOnly), EndDate: endDate,
						}, now)
						if err != nil {
							t.Fatal(err)
						}
						request.Start, request.End = validated.Start, validated.End
					} else {
						request.Start, request.End, err = marketdata.CompletedDailyRange(now)
						if err != nil {
							t.Fatal(err)
						}
					}
					if request.End.UTC().Format(time.DateOnly) == endDate {
						t.Fatal("fixture does not cross the UTC calendar boundary")
					}
					barsCalled, rateCalled := false, false
					client := marketdata.NewClient()
					client.AlpacaURL, client.FREDURL = "https://provider.test/bars", "https://provider.test/rate"
					client.HTTPClient = &http.Client{Transport: calendarTransport(func(r *http.Request) (*http.Response, error) {
						var body string
						switch r.URL.Path {
						case "/bars":
							barsCalled = true
							if r.URL.Query().Get("start") != request.Start.UTC().Format(time.RFC3339) || r.URL.Query().Get("end") != request.End.UTC().Format(time.RFC3339) {
								t.Fatal("Alpaca timestamp bounds changed")
							}
							var bars []string
							for i := -2; i <= 0; i++ {
								bars = append(bars, fmt.Sprintf(`{"o":100,"h":100,"l":100,"c":100,"v":1000,"vw":100,"t":%q}`, last.AddDate(0, 0, i).UTC().Format(time.RFC3339)))
							}
							body = `{"bars":{"AAPL":[` + strings.Join(bars, ",") + `]}}`
						case "/rate":
							rateCalled = true
							if got := r.URL.Query().Get("observation_end"); got != endDate {
								t.Errorf("FRED observation_end=%s, want %s", got, endDate)
							}
							observationDate := endDate
							if futureObservation {
								observationDate = last.AddDate(0, 0, 1).Format(time.DateOnly)
							}
							body = fmt.Sprintf(`{"observations":[{"date":%q,"value":"4.25"}]}`, observationDate)
						default:
							t.Fatalf("unexpected provider path %s", r.URL.Path)
						}
						return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
					})}
					settings := backtest.DefaultServiceSettings()
					settings.MonteCarlo.Seed, settings.MonteCarlo.NumPaths = 42, 10
					service, err := backtest.NewService(client, backtest.Credentials{AlpacaKey: "test", AlpacaSecret: "test", FREDKey: "test"}, settings)
					if err != nil {
						t.Fatal(err)
					}
					output, err := service.Run(context.Background(), request)
					if !barsCalled || !rateCalled {
						t.Fatal("provider boundary was not exercised")
					}
					if futureObservation {
						if err == nil || err.Error() != "provider_error" {
							t.Fatalf("following-day observation accepted: %v", err)
						}
					} else if err != nil || output.Config.MonteCarloInput.RiskFreeRate != .0425 {
						t.Fatalf("rate=%v err=%v", output.Config.MonteCarloInput.RiskFreeRate, err)
					}
				})
			}
		}
	}
}
