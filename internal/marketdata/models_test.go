package marketdata

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func testClient(fn func(*http.Request) (int, string)) *Client {
	c := NewClient()
	c.HTTPClient = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		code, body := fn(r)
		return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	return c
}
func TestFetchBarsPagination(t *testing.T) {
	calls := 0
	c := testClient(func(r *http.Request) (int, string) {
		calls++
		q := r.URL.Query()
		if q.Get("feed") != "sip" || q.Get("sort") != "asc" || q.Get("symbols") != "AAPL" || r.Header.Get("APCA-API-KEY-ID") != "key" {
			t.Fatal("incorrect request")
		}
		if calls == 2 && q.Get("page_token") != "next" {
			t.Fatal("missing page token")
		}
		token := "next"
		if calls == 2 {
			token = ""
		}
		return 200, fmt.Sprintf(`{"bars":{"AAPL":[{"o":100,"h":102,"l":99,"c":101,"v":1000,"vw":100.5,"t":"2025-01-0%dT05:00:00Z"}]},"next_page_token":%q}`, calls+1, token)
	})
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	bars, err := c.FetchBars(context.Background(), " aapl ", start, start.AddDate(0, 0, 5), "key", "secret")
	if err != nil || len(bars) != 2 || calls != 2 {
		t.Fatalf("bars=%d calls=%d err=%v", len(bars), calls, err)
	}
}
func TestProviderFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"unauthorized", 401, `secret`}, {"rate limit", 429, `secret`}, {"server", 500, `secret`}, {"json", 200, `{`}, {"empty", 200, `{"bars":{}}`},
		{"repeated token", 200, `{"next_page_token":"same"}`},
		{"bad price", 200, `{"bars":{"AAPL":[{"t":"2025-01-02T05:00:00Z","o":0}]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testClient(func(*http.Request) (int, string) { return tc.status, tc.body })
			start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
			_, err := c.FetchBars(context.Background(), "AAPL", start, start.AddDate(0, 0, 5), "key", "secret")
			if err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("unsafe/missing error %v", err)
			}
		})
	}
}
func TestFRED(t *testing.T) {
	for _, tc := range []struct {
		body string
		ok   bool
	}{
		{`{"observations":[{"date":"2025-01-03","value":"."},{"date":"2025-01-02","value":"4.25"}]}`, true},
		{`{"observations":[{"date":"2025-01-02","value":"NaN"}]}`, false},
		{`{"observations":[{"date":"2025-01-02","value":"+Inf"}]}`, false},
		{`{"observations":[{"date":"2025-01-05","value":"4"}]}`, false},
		{`{"observations":[{"date":"bad","value":"4"}]}`, false},
		{`{"observations":[]}`, false},
	} {
		c := testClient(func(r *http.Request) (int, string) {
			if r.URL.Query().Get("series_id") != "DGS3MO" {
				t.Fatal("wrong series")
			}
			return 200, tc.body
		})
		rate, err := c.FetchRiskFreeRate(context.Background(), "secret", time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC))
		if (err == nil) != tc.ok || (tc.ok && math.Abs(rate-.0425) > 1e-12) {
			t.Fatalf("rate=%v err=%v", rate, err)
		}
	}
}
func TestTransportErrorRedactsCredentials(t *testing.T) {
	c := NewClient()
	c.HTTPClient = &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("secret") })}
	_, err := c.FetchRiskFreeRate(context.Background(), "secret", time.Now())
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("error=%v", err)
	}
}
func TestValidateBars(t *testing.T) {
	good := BarTick{Symbol: "AAPL", Open: 100, High: 102, Low: 99, Close: 101, Volume: 100, VWAP: 100, Timestamp: time.Date(2025, 1, 2, 5, 0, 0, 0, time.UTC)}
	for name, mutate := range map[string]func(*BarTick){
		"nan": func(b *BarTick) { b.Close = math.NaN() }, "negative volume": func(b *BarTick) { b.Volume = -1 }, "zero price": func(b *BarTick) { b.Open = 0 }, "range": func(b *BarTick) { b.Low = 101 }, "timestamp": func(b *BarTick) { b.Timestamp = time.Time{} }, "vwap": func(b *BarTick) { b.VWAP = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			b := good
			mutate(&b)
			if ValidateBars("AAPL", []BarTick{b}) == nil {
				t.Fatal("accepted invalid bar")
			}
		})
	}
	if ValidateBars("AAPL", []BarTick{good, good}) == nil {
		t.Fatal("accepted duplicate date")
	}
	good.Volume = 0
	good.VWAP = 0
	if err := ValidateBars("AAPL", []BarTick{good}); err != nil {
		t.Fatal(err)
	}
}
func TestCompletedDailyRange(t *testing.T) {
	for _, date := range []string{"2026-03-09T16:00:00Z", "2026-11-02T16:00:00Z", "2026-09-05T16:00:00Z"} {
		now, _ := time.Parse(time.RFC3339, date)
		start, end, err := CompletedDailyRange(now)
		if err != nil {
			t.Fatal(err)
		}
		loc, _ := time.LoadLocation("America/New_York")
		if end.In(loc).Hour() != 23 || end.In(loc).Day() == now.In(loc).Day() || !start.Before(end) {
			t.Fatalf("bad range %s %s", start, end)
		}
	}
}

func TestCompletedDailyRangeJustAfterMidnight(t *testing.T) {
	now, _ := time.Parse(time.RFC3339, "2026-09-05T04:05:00Z")
	_, end, err := CompletedDailyRange(now)
	if err != nil {
		t.Fatal(err)
	}
	if now.Sub(end) < 15*time.Minute {
		t.Fatal("end falls inside SIP recent-data restriction")
	}
}
