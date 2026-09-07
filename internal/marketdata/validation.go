package marketdata

import (
	"fmt"
	"math"
	"time"
)

// ValidateBars validates one daily series without sorting, dropping, or repairing data.
// Zero-volume bars may have zero VWAP; they contribute no weight to indicators.
func ValidateBars(symbol string, bars []BarTick) error {
	if !validTicker.MatchString(symbol) {
		return fmt.Errorf("invalid symbol %q", symbol)
	}
	if len(bars) == 0 {
		return fmt.Errorf("no bars for %s", symbol)
	}
	previous := ""
	for i, b := range bars {
		fail := func() error { return fmt.Errorf("%s bar %d is invalid", symbol, i) }
		if b.Symbol != symbol || b.Timestamp.IsZero() {
			return fail()
		}
		date := b.Timestamp.UTC().Format(time.DateOnly)
		if date <= previous {
			return fmt.Errorf("%s bar %d has duplicate or unordered date", symbol, i)
		}
		previous = date
		for _, v := range []float64{b.Open, b.High, b.Low, b.Close, b.Volume, b.VWAP} {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return fail()
			}
		}
		if b.Open <= 0 || b.High <= 0 || b.Low <= 0 || b.Close <= 0 || b.Volume < 0 || b.VWAP < 0 || (b.Volume > 0 && b.VWAP == 0) {
			return fail()
		}
		if b.Low > b.High || b.Open < b.Low || b.Open > b.High || b.Close < b.Low || b.Close > b.High {
			return fail()
		}
	}
	return nil
}

// CompletedDailyRange excludes today's US session, including partial daily bars.
// On weekends and holidays the provider simply returns no bar for those dates.
func CompletedDailyRange(now time.Time) (time.Time, time.Time, error) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	// Keep the range outside SIP's recent-data restriction even just after midnight.
	local := now.Add(-15 * time.Minute).In(location)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	return today.AddDate(-1, 0, 0).UTC(), today.Add(-time.Second).UTC(), nil
}
