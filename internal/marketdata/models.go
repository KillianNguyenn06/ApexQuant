package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var validTicker = regexp.MustCompile(`^[A-Z][A-Z0-9.-]{0,9}$`)

type BarTick struct {
	Symbol    string    `json:"symbol"`
	Open      float64   `json:"open"`
	High      float64   `json:"high"`
	Low       float64   `json:"low"`
	Close     float64   `json:"close"`
	Volume    float64   `json:"volume"`
	VWAP      float64   `json:"vwap"`
	Timestamp time.Time `json:"timestamp"`
}

type alpacaBar struct {
	Open      float64   `json:"o"`
	High      float64   `json:"h"`
	Low       float64   `json:"l"`
	Close     float64   `json:"c"`
	Volume    float64   `json:"v"`
	VWAP      float64   `json:"vw"`
	Timestamp time.Time `json:"t"`
}

type alpacaResponse struct {
	Bars          map[string][]alpacaBar `json:"bars"`
	NextPageToken string                 `json:"next_page_token"`
}

// =================================================
// Fetch API for Bar Ticks
// =================================================
// Client keeps provider configuration outside the backtest engine.
type Client struct {
	HTTPClient *http.Client
	AlpacaURL  string
	FREDURL    string
}

func NewClient() *Client {
	return &Client{HTTPClient: &http.Client{Timeout: 15 * time.Second}, AlpacaURL: "https://data.alpaca.markets/v2/stocks/bars", FREDURL: "https://api.stlouisfed.org/fred/series/observations"}
}

func FetchAPI(symbol string, start, end time.Time, apiKey, apiSecret string) ([]BarTick, error) {
	return NewClient().FetchBars(context.Background(), symbol, start, end, apiKey, apiSecret)
}

func (c *Client) FetchBars(ctx context.Context, symbol string, start, end time.Time, apiKey, apiSecret string) ([]BarTick, error) {
	if start.IsZero() || end.IsZero() || !start.Before(end) {
		return nil, fmt.Errorf("start must precede end")
	}

	if apiKey == "" {
		return nil, fmt.Errorf("\n\tError: APCA_API_KEY_ID is empty\n")
	} else if apiSecret == "" {
		return nil, fmt.Errorf("\n\tError: APCA_API_SECRET_KEY is empty\n")
	}

	symbol = strings.ToUpper(strings.TrimSpace(symbol))

	if !validTicker.MatchString(symbol) {
		return nil, fmt.Errorf("\n\tError: Invalid ticker: %q", symbol)
	}

	endpoint, err := url.Parse(
		c.AlpacaURL,
	)
	if err != nil {
		return nil, err
	}

	query := endpoint.Query()
	query.Set("symbols", symbol)
	query.Set("timeframe", "1Day")
	query.Set("start", start.UTC().Format(time.RFC3339))
	query.Set("end", end.UTC().Format(time.RFC3339))
	query.Set("limit", "1000")
	query.Set("feed", "sip")
	query.Set("sort", "asc")
	query.Set("adjustment", "raw")
	var bars []BarTick
	seen := map[string]bool{}
	for {
		endpoint.RawQuery = query.Encode()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("invalid Alpaca request")
		}
		request.Header.Set("APCA-API-KEY-ID", apiKey)
		request.Header.Set("APCA-API-SECRET-KEY", apiSecret)
		var payload alpacaResponse
		if err := c.getJSON(request, &payload, "Alpaca"); err != nil {
			return nil, err
		}
		for _, bar := range payload.Bars[symbol] {
			bars = append(bars, BarTick{Symbol: symbol, Open: bar.Open, High: bar.High, Low: bar.Low, Close: bar.Close, Volume: bar.Volume, VWAP: bar.VWAP, Timestamp: bar.Timestamp})
		}
		token := payload.NextPageToken
		if token == "" {
			break
		}
		if seen[token] {
			return nil, fmt.Errorf("Alpaca repeated pagination token")
		}
		seen[token] = true
		query.Set("page_token", token)
	}
	if err := ValidateBars(symbol, bars); err != nil {
		return nil, err
	}
	for _, bar := range bars {
		if bar.Timestamp.Before(start) || bar.Timestamp.After(end) {
			return nil, fmt.Errorf("%s bar outside requested range", symbol)
		}
	}
	return bars, nil
}

// getJSON never exposes provider URLs or response bodies, which may contain credentials.
func (c *Client) getJSON(request *http.Request, target any, provider string) error {
	response, err := c.HTTPClient.Do(request)
	if err != nil {
		return fmt.Errorf("%s request failed", provider)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned status %d", provider, response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return fmt.Errorf("%s returned invalid JSON", provider)
	}
	return nil
}

type fredObservation struct {
	Date  string `json:"date"`
	Value string `json:"value"`
}

type fredResponse struct {
	Observations []fredObservation `json:"observations"`
}

// =================================================
// Fetch API for Risk Free Rate
// =================================================
func FetchRiskFreeRate(apiKey string, asOf time.Time) (float64, error) {
	return NewClient().FetchRiskFreeRate(context.Background(), apiKey, asOf)
}

func (c *Client) FetchRiskFreeRate(ctx context.Context, apiKey string, asOf time.Time) (float64, error) {
	if asOf.IsZero() {
		return 0, fmt.Errorf("as-of date is required")
	}

	if apiKey == "" {
		return 0, fmt.Errorf("\n\tError: FRED_API_KEY is empty\n")
	}
	endpoint, err := url.Parse(
		c.FREDURL,
	)
	if err != nil {
		return 0, err
	}

	query := endpoint.Query()
	query.Set("series_id", "DGS3MO")
	query.Set("api_key", apiKey)
	query.Set("file_type", "json")
	query.Set("observation_end", asOf.Format("2006-01-02"))
	query.Set("sort_order", "desc")
	query.Set("limit", "10")
	endpoint.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return 0, fmt.Errorf("invalid FRED request")
	}
	var payload fredResponse
	if err := c.getJSON(request, &payload, "FRED"); err != nil {
		return 0, err
	}

	for _, observation := range payload.Observations {
		date, err := time.Parse("2006-01-02", observation.Date)
		if err != nil || date.Format("2006-01-02") > asOf.Format("2006-01-02") {
			return 0, fmt.Errorf("invalid FRED observation date")
		}
		if observation.Value == "." {
			continue
		}

		percentage, err := strconv.ParseFloat(
			observation.Value,
			64,
		)
		if err != nil || math.IsNaN(percentage) || math.IsInf(percentage, 0) {
			return 0, fmt.Errorf("invalid FRED rate")
		}

		return percentage / 100, nil
	}

	return 0, fmt.Errorf("no risk-free rate available")
}
