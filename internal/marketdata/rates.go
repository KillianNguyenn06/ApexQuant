package marketdata

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"
)

// RateObservation records a vintage, not just today's revised historical value.
// AvailableDate is conservative: the calendar day after BOTH observation and
// vintage dates, since FRED dates do not establish intraday publication timing.
type RateObservation struct {
	Date          string  `json:"date"`
	AvailableDate string  `json:"available_date"`
	Rate          float64 `json:"rate"`
}

// RateAt never backfills from the future. Revisions of older observations do not
// displace a newer available observation. Dates use the caller's market calendar.
func RateAt(history []RateObservation, date string) (RateObservation, bool) {
	var selected RateObservation
	found := false
	for _, r := range history {
		if r.Date >= date || r.AvailableDate > date {
			continue
		}
		if !found || r.Date > selected.Date || (r.Date == selected.Date && r.AvailableDate > selected.AvailableDate) {
			selected = r
			found = true
		}
	}
	return selected, found
}

// FetchRiskFreeRates retrieves dated vintages in batches; cache and throttle are
// shared by all runs using this Client. Identical concurrent fetches coalesce.
func (c *Client) FetchRiskFreeRates(ctx context.Context, apiKey string, start, end time.Time) ([]RateObservation, error) {
	if apiKey == "" || start.IsZero() || end.Before(start) {
		return nil, fmt.Errorf("invalid FRED history request")
	}
	c.fredMu.Lock()
	defer c.fredMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%x:%s:%s:%s", sha256.Sum256([]byte(apiKey)), c.FREDURL, start.Format(time.DateOnly), end.Format(time.DateOnly))
	if cached, ok := c.rateCache[key]; ok {
		return append([]RateObservation(nil), cached...), nil
	}
	endpoint, err := url.Parse(c.FREDURL)
	if err != nil {
		return nil, fmt.Errorf("invalid FRED endpoint")
	}
	q := endpoint.Query()
	q.Set("series_id", "DGS3MO")
	q.Set("api_key", apiKey)
	q.Set("file_type", "json")
	q.Set("observation_start", start.Format(time.DateOnly))
	q.Set("observation_end", end.Format(time.DateOnly))
	// FRED caps JSON requests at 2,000 vintage dates, even when observation
	// dates are narrow. Four calendar years contain at most 1,461 dates.
	// Start with the preparation range, not all vintages since 1776.
	vintageStart := start
	q.Set("output_type", "1")
	q.Set("sort_order", "asc")
	q.Set("limit", "100000")
	var rates []RateObservation
	for {
		vintageEnd := vintageStart.AddDate(4, 0, 0).AddDate(0, 0, -1)
		if vintageEnd.After(end) {
			vintageEnd = end
		}
		q.Set("realtime_start", vintageStart.Format(time.DateOnly))
		q.Set("realtime_end", vintageEnd.Format(time.DateOnly))
		for offset := 0; ; {
			q.Set("offset", strconv.Itoa(offset))
			endpoint.RawQuery = q.Encode()
			var payload struct {
				Count        int `json:"count"`
				Observations []struct {
					Date    string `json:"date"`
					Vintage string `json:"realtime_start"`
					Value   string `json:"value"`
				} `json:"observations"`
			}
			for attempt := 0; ; attempt++ {
				if err := waitRate(ctx, time.Until(c.nextFRED)); err != nil {
					return nil, err
				}
				c.nextFRED = time.Now().Add(600 * time.Millisecond) // <=100/minute within this client.
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
				if err != nil {
					return nil, fmt.Errorf("invalid FRED request")
				}
				response, err := c.HTTPClient.Do(req)
				if err != nil {
					return nil, fmt.Errorf("FRED request failed")
				}
				if response.StatusCode == http.StatusTooManyRequests && attempt < 2 {
					delay := time.Duration(1<<attempt) * time.Second
					if seconds, e := strconv.Atoi(response.Header.Get("Retry-After")); e == nil && seconds > 0 {
						delay = max(delay, time.Duration(seconds)*time.Second)
					}
					response.Body.Close()
					// Bound this operation rather than retrying before a long server deadline.
					if delay > 30*time.Second {
						return nil, fmt.Errorf("FRED rate limited")
					}
					if err := waitRate(ctx, delay); err != nil {
						return nil, err
					}
					continue
				}
				if response.StatusCode != http.StatusOK {
					response.Body.Close()
					return nil, fmt.Errorf("FRED returned status %d", response.StatusCode)
				}
				err = json.NewDecoder(response.Body).Decode(&payload)
				response.Body.Close()
				if err != nil {
					return nil, fmt.Errorf("FRED returned invalid JSON")
				}
				break
			}
			for _, o := range payload.Observations {
				date, e1 := time.Parse(time.DateOnly, o.Date)
				vintage, e2 := time.Parse(time.DateOnly, o.Vintage)
				if e1 != nil || e2 != nil || o.Date < start.Format(time.DateOnly) || o.Date > end.Format(time.DateOnly) || o.Vintage < vintageStart.Format(time.DateOnly) || o.Vintage > vintageEnd.Format(time.DateOnly) {
					return nil, fmt.Errorf("invalid FRED history date")
				}
				if o.Value == "." {
					continue
				}
				rate, err := strconv.ParseFloat(o.Value, 64)
				if err != nil || math.IsNaN(rate) || math.IsInf(rate, 0) {
					return nil, fmt.Errorf("invalid FRED rate")
				}
				available := date
				if vintage.After(available) {
					available = vintage
				}
				rates = append(rates, RateObservation{Date: o.Date, AvailableDate: available.AddDate(0, 0, 1).Format(time.DateOnly), Rate: rate / 100})
			}
			offset += len(payload.Observations)
			if offset >= payload.Count {
				break
			}
			if len(payload.Observations) == 0 || offset > 1000000 {
				return nil, fmt.Errorf("incomplete FRED history")
			}
		}
		if !vintageEnd.Before(end) {
			break
		}
		vintageStart = vintageEnd.AddDate(0, 0, 1)
	}
	if len(rates) == 0 {
		return nil, fmt.Errorf("no risk-free history available")
	}
	sort.SliceStable(rates, func(i, j int) bool {
		if rates[i].Date == rates[j].Date {
			return rates[i].AvailableDate < rates[j].AvailableDate
		}
		return rates[i].Date < rates[j].Date
	})
	if c.rateCache == nil {
		c.rateCache = make(map[string][]RateObservation)
	}
	// Bound cached date ranges; callers retain independent copies.
	if len(c.rateCache) >= 8 {
		c.rateCache = make(map[string][]RateObservation)
	}
	c.rateCache[key] = append([]RateObservation(nil), rates...)
	return rates, nil
}
func waitRate(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
