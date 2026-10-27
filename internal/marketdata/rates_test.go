package marketdata

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type rateTransport func(*http.Request) (*http.Response, error)

func (f rateTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRateHistoryBatchCacheAndVintages(t *testing.T) {
	var calls atomic.Int32
	c := NewClient()
	c.HTTPClient = &http.Client{Transport: rateTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		q := r.URL.Query()
		if q.Get("output_type") != "1" || q.Get("realtime_start") != "2024-01-01" || q.Get("observation_start") != "2024-01-01" || q.Get("observation_end") != "2024-02-01" {
			t.Error("incorrect batch/vintage request")
		}
		body := `{"count":3,"observations":[{"date":"2024-01-02","realtime_start":"2024-01-02","value":"4.2"},{"date":"2024-01-02","realtime_start":"2024-01-10","value":"4.3"},{"date":"2024-01-05","realtime_start":"2024-01-05","value":"4.4"}]}`
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	start, _ := time.Parse(time.DateOnly, "2024-01-01")
	end, _ := time.Parse(time.DateOnly, "2024-02-01")
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			history, e := c.FetchRiskFreeRates(context.Background(), "test", start, end)
			if e != nil || len(history) != 3 {
				t.Errorf("history %v %v", history, e)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("identical concurrent requests not coalesced")
	}
	history, _ := c.FetchRiskFreeRates(context.Background(), "test", start, end)
	if _, ok := RateAt(history, "2024-01-02"); ok {
		t.Fatal("same-day unavailable rate accepted")
	}
	rate, ok := RateAt(history, "2024-01-03")
	if !ok || rate.Rate != .042 {
		t.Fatal("wrong historical vintage")
	}
	rate, ok = RateAt(history, "2024-01-11")
	if !ok || math.Abs(rate.Rate-.044) > 1e-12 {
		t.Fatal("older revised observation displaced newer one")
	}
	history[0].Rate = 99
	again, _ := c.FetchRiskFreeRates(context.Background(), "test", start, end)
	if again[0].Rate == 99 {
		t.Fatal("caller mutated cache")
	}
}
func TestHistoryRejectsInvalidProviderData(t *testing.T) {
	for _, row := range []string{`{"date":"2024-02-02","realtime_start":"2024-02-02","value":"4"}`, `{"date":"2024-01-02","realtime_start":"2024-01-02","value":"NaN"}`, `{"date":"2024-01-02","value":"4"}`} {
		c := NewClient()
		c.HTTPClient = &http.Client{Transport: rateTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"observations":[%s]}`, row)))}, nil
		})}
		start, _ := time.Parse(time.DateOnly, "2024-01-01")
		end, _ := time.Parse(time.DateOnly, "2024-02-01")
		if _, e := c.FetchRiskFreeRates(context.Background(), "test", start, end); e == nil {
			t.Fatal("invalid history accepted")
		}
	}
}
func TestRateLimiterCancellation(t *testing.T) {
	c := NewClient()
	c.nextFRED = time.Now().Add(time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now().AddDate(0, 0, -30)
	if _, e := c.FetchRiskFreeRates(ctx, "test", start, time.Now()); e == nil {
		t.Fatal("canceled fetch ran")
	}
}
func TestRateHistory429Retries(t *testing.T) {
	var calls atomic.Int32
	c := NewClient()
	c.HTTPClient = &http.Client{Transport: rateTransport(func(*http.Request) (*http.Response, error) {
		n := calls.Add(1)
		status := 200
		body := `{"observations":[{"date":"2024-01-02","realtime_start":"2024-01-02","value":"4"}]}`
		if n == 1 {
			status = 429
			body = `{}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	start, _ := time.Parse(time.DateOnly, "2024-01-01")
	end, _ := time.Parse(time.DateOnly, "2024-02-01")
	if _, e := c.FetchRiskFreeRates(context.Background(), "test", start, end); e != nil || calls.Load() != 2 {
		t.Fatalf("retry %v calls %d", e, calls.Load())
	}
}

// The one-year screenshot must not query every vintage since 1776. Long runs
// also need bounded vintage windows, separately from observation pagination.
func TestRateHistoryBoundsVintageWindowsAndPaginates(t *testing.T) {
	var windows []string
	requests := 0
	c := NewClient()
	c.HTTPClient = &http.Client{Transport: rateTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		q := r.URL.Query()
		from, e1 := time.Parse(time.DateOnly, q.Get("realtime_start"))
		to, e2 := time.Parse(time.DateOnly, q.Get("realtime_end"))
		if e1 != nil || e2 != nil || to.Before(from) || to.Sub(from) > 1461*24*time.Hour {
			t.Fatal("vintage limit can be exceeded")
		}
		if q.Get("observation_start") != "2017-01-01" || q.Get("observation_end") != "2026-10-09" {
			t.Fatal("observation range changed across vintage windows")
		}
		if q.Get("offset") == "0" {
			windows = append(windows, from.Format(time.DateOnly))
		} else if q.Get("offset") != "1" {
			t.Fatal("pagination cursor not reset for each window")
		}
		body := fmt.Sprintf(`{"count":2,"observations":[{"date":%q,"realtime_start":%q,"value":"4.2"}]}`, from.Format(time.DateOnly), from.Format(time.DateOnly))
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	start, _ := time.Parse(time.DateOnly, "2017-01-01")
	end, _ := time.Parse(time.DateOnly, "2026-10-09")
	rates, e := c.FetchRiskFreeRates(context.Background(), "test", start, end)
	if e != nil || requests != 6 || len(rates) != 6 || strings.Join(windows, ",") != "2017-01-01,2021-01-01,2025-01-01" {
		t.Fatalf("windows %v requests %d rates %d err %v", windows, requests, len(rates), e)
	}
	// No later vintage is used to invent an earlier available rate.
	if _, ok := RateAt(rates, "2017-01-01"); ok {
		t.Fatal("initial-day unavailable rate used")
	}
	if r, ok := RateAt(rates, "2020-12-31"); !ok || r.Date != "2017-01-01" {
		t.Fatal("next window leaked backward")
	}
}
