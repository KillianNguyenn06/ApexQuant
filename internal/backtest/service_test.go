package backtest

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"apexquant/internal/marketdata"
	"apexquant/internal/session"
)

type fakeProvider struct {
	bars  map[string][]marketdata.BarTick
	rate  float64
	asOf  time.Time
	fail  bool
	calls int
}

func (p *fakeProvider) FetchBars(ctx context.Context, symbol string, start, end time.Time, key, secret string) ([]marketdata.BarTick, error) {
	p.calls++
	if p.fail {
		return nil, errors.New("provider leaked secret")
	}
	return p.bars[symbol], nil
}
func (p *fakeProvider) FetchRiskFreeRate(ctx context.Context, key string, asOf time.Time) (float64, error) {
	p.asOf = asOf
	return p.rate, nil
}
func TestServiceMatchesDirectEngine(t *testing.T) {
	p := &fakeProvider{rate: .04, bars: map[string][]marketdata.BarTick{}}
	for _, symbol := range []string{"AAPL", "MSFT"} {
		for i := 0; i < 4; i++ {
			p.bars[symbol] = append(p.bars[symbol], testBar(symbol, i, 100, 100, 100))
		}
	}
	settings := DefaultServiceSettings()
	settings.MonteCarlo.Seed = 42
	settings.MonteCarlo.NumPaths = 10
	service, err := NewService(p, Credentials{}, settings)
	if err != nil {
		t.Fatal(err)
	}
	request := RunRequest{InitialCapital: 50000, Allocations: []session.PortfolioAllocation{{Symbol: " aapl ", Weight: .5}, {Symbol: "MSFT", Weight: .5}}, Start: p.bars["AAPL"][0].Timestamp, End: p.bars["AAPL"][3].Timestamp}
	output, err := service.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	direct, err := RunBacktest(output.Config)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(output.Result, direct) || !p.asOf.Equal(request.End) || p.asOf.Location().String() != "America/New_York" || request.Allocations[0].Symbol != " aapl " {
		t.Fatal("orchestration changed results, as-of date, or input")
	}
}
func TestServiceFailures(t *testing.T) {
	p := &fakeProvider{fail: true}
	s, _ := NewService(p, Credentials{}, DefaultServiceSettings())
	r := RunRequest{InitialCapital: 50000, Allocations: []session.PortfolioAllocation{{Symbol: "AAPL", Weight: 1}}, Start: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)}
	_, err := s.Run(context.Background(), r)
	if err == nil || err.Error() != "provider_error" {
		t.Fatalf("unsafe provider error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.calls = 0
	if _, err = s.Run(ctx, r); err == nil || err.Error() != "canceled" || p.calls != 0 {
		t.Fatal("canceled request fetched data")
	}
	r.Allocations = append(r.Allocations, session.PortfolioAllocation{Symbol: " aapl ", Weight: 1})
	if _, err = s.Run(context.Background(), r); err == nil || err.Error() != "invalid_request" {
		t.Fatal("duplicate accepted")
	}
}
