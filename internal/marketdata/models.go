package marketdata

import (
	"encoding/json"
	"fmt"
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
	Bars map[string][]alpacaBar `json:"bars"`
}

func FetchAPI(symbol string, start time.Time, end time.Time, apiKey string, apiSecret string) ([]BarTick, error) {

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
		"https://data.alpaca.markets/v2/stocks/bars",
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
	query.Set("feed", "iex")
	query.Set("adjustment", "raw")
	endpoint.RawQuery = query.Encode()

	request, err := http.NewRequest(http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}

	request.Header.Set("APCA-API-KEY-ID", apiKey)
	request.Header.Set("APCA-API-SECRET-KEY", apiSecret)

	client := http.Client{Timeout: 15 * time.Second}

	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Alpaca returned status: %s", response.Status)
	}

	var payload alpacaResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}

	apiBars := payload.Bars[symbol]
	bars := make([]BarTick, 0, len(apiBars))

	for _, bar := range apiBars {
		bars = append(bars, BarTick{
			Symbol:    symbol,
			Open:      bar.Open,
			High:      bar.High,
			Low:       bar.Low,
			Close:     bar.Close,
			Volume:    bar.Volume,
			VWAP:      bar.VWAP,
			Timestamp: bar.Timestamp,
		})
	}
	return bars, nil
}

type fredObservation struct {
	Date  string `json:"date"`
	Value string `json:"value"`
}

type fredResponse struct {
	Observations []fredObservation `json:"observations"`
}

func FetchRiskFreeRate(apiKey string, asOf time.Time) (float64, error) {

	if apiKey == "" {
		return 0, fmt.Errorf("\n\tError: FRED_API_KEY is empty\n")
	}
	endpoint, err := url.Parse(
		"https://api.stlouisfed.org/fred/series/observations",
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

	client := http.Client{Timeout: 15 * time.Second}

	response, err := client.Get(endpoint.String())
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return 0, fmt.Errorf(
			"FRED returned status %s",
			response.Status,
		)
	}

	var payload fredResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return 0, err
	}

	for _, observation := range payload.Observations {
		if observation.Value == "." {
			continue
		}

		percentage, err := strconv.ParseFloat(
			observation.Value,
			64,
		)
		if err != nil {
			return 0, err
		}

		return percentage / 100, nil
	}

	return 0, fmt.Errorf("no risk-free rate available")
}
