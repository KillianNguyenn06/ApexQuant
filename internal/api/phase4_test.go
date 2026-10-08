package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"apexquant/internal/backtest"
	"apexquant/internal/marketdata"
)

type phase4Provider struct{ 
	zeroVolume bool 
}

func (p phase4Provider) FetchBars(_ context.Context, symbol string, start, _ time.Time, _, _ string) ([]marketdata.BarTick, error) {
	prices := [][3]float64{{100, 100, 100}, {100, 90, 110}, {91, 92, 100}, {93, 94, 100}, {95, 110, 100}, {109, 109, 100}}
	if symbol == "MSFT" {
		prices = [][3]float64{{200, 200, 200}, {200, 200, 200}, {200, 200, 200}, {200, 200, 200}, {200, 200, 200}, {200, 200, 200}}
	}
	bars := make([]marketdata.BarTick, len(prices))
	for i, price := range prices {
		stamp := start.AddDate(0, 0, i)
		if symbol == "AAPL" {
			stamp = stamp.Add(2 * time.Hour)
		}
		bars[i] = marketdata.BarTick{Symbol: symbol, Timestamp: stamp, Open: price[0], Close: price[1], High: math.Max(price[0], price[1]), Low: math.Min(price[0], price[1]), VWAP: price[2], Volume: 1000}
		if p.zeroVolume && symbol == "AAPL" && (i < 2 || i == 5) {
			bars[i].VWAP, bars[i].Volume = 0, 0
		}
	}
	return bars, nil
}
func (phase4Provider) FetchRiskFreeRate(context.Context, string, time.Time) (float64, error) {
	return 1, nil
}

func TestPhase4ReplayValuesAccountingAndAvailability(t *testing.T) {
	for _, http2 := range []bool{false, true} {
		for _, zeroVolume := range []bool{false, true} {
			t.Run(fmt.Sprintf("http2=%t/zeroVolume=%t", http2, zeroVolume), func(t *testing.T) {
				_, settings := tradingService(t, tradingProvider{})
				service, err := backtest.NewService(phase4Provider{zeroVolume: zeroVolume}, backtest.Credentials{}, settings)
				if err != nil {
					t.Fatal(err)
				}
				h, m := httpTestHandler(t, ServiceRunner(service, DataInfo{Feed: "sip", Timeframe: "1Day", Adjustment: "raw"}))
				server := httptest.NewUnstartedServer(h)
				server.EnableHTTP2 = http2
				if http2 {
					server.StartTLS()
				} else {
					server.Start()
				}
				defer server.Close()
				client := server.Client()
				client.Timeout = 3 * time.Second
				response, err := client.Post(server.URL+"/api/backtests", "application/json", strings.NewReader(requestBody))
				if err != nil {
					t.Fatal(err)
				}
				var created CreateBacktestResponse
				err = json.NewDecoder(response.Body).Decode(&created)
				response.Body.Close()
				if err != nil || response.StatusCode != 202 {
					t.Fatal("HTTP admission failed")
				}
				awaitStatus(t, m, created.ID, StatusCompleted)
				response, err = client.Get(server.URL + "/api/backtests/" + created.ID + "/result")
				if err != nil {
					t.Fatal(err)
				}
				var completed BacktestResultResponse
				err = json.NewDecoder(response.Body).Decode(&completed)
				response.Body.Close()
				if err != nil || response.StatusCode != 200 {
					t.Fatal("HTTP result failed")
				}
				response, err = client.Get(server.URL + "/api/backtests/" + created.ID + "/replay?speed=20")
				if err != nil {
					t.Fatal(err)
				}
				events, err := readSSE(response.Body)
				response.Body.Close()
				if err != nil || response.StatusCode != 200 || (http2 && response.ProtoMajor != 2) {
					t.Fatalf("HTTP replay: %v protocol=%s", err, response.Proto)
				}
				if len(events) != 20 {
					t.Fatalf("events=%d, want 20", len(events))
				}
				cash := completed.Request.InitialCapital
				quantities := map[string]float64{"AAPL": 0, "MSFT": 0}
				pending := map[string]*Order{}
				buys, sells, holdingDays := 0, 0, 0
				near := func(got, want float64) {
					t.Helper()
					if math.Abs(got-want) > 1e-8*math.Max(1, math.Abs(want)) {
						t.Fatalf("got %v want %v", got, want)
					}
				}
				for day, date := range []string{"2025-09-05", "2025-09-06", "2025-09-07", "2025-09-08", "2025-09-09", "2025-09-10"} {
					for symbolIndex, symbol := range []string{"AAPL", "MSFT"} {
						index := 1 + day*3 + symbolIndex
						event := events[index]
						data := decodeReplay[ReplaySnapshotData](t, event)
						s := data.Snapshot
						if event.ID != index+1 || event.Type != ReplaySnapshot || data.DayIndex != day || data.SnapshotIndex != day*2+symbolIndex || s.Bar.Symbol != symbol || s.Timestamp.Format(time.DateOnly) != date {
							t.Fatal("wire sequence differs from independently specified days and stocks")
						}
						gotJSON, _ := json.Marshal(s)
						wantJSON, _ := json.Marshal(completed.Snapshots[day*2+symbolIndex])
						if string(gotJSON) != string(wantJSON) {
							t.Fatal("stream changed a completed snapshot")
						}
						if fill := s.FilledOrder; fill != nil {
							order := pending[symbol]
							if order == nil || order.ID != fill.ID || order.CreatedAt.UTC().Format(time.DateOnly) >= date || fill.FilledPrice == nil || *fill.FilledPrice != s.Bar.Open {
								t.Fatal("next-open order guarantee changed in replay")
							}
							if fill.Action == "buy" {
								cash -= fill.Quantity * *fill.FilledPrice
								quantities[symbol] += fill.Quantity
								buys++
							} else {
								cash += fill.Quantity * *fill.FilledPrice
								quantities[symbol] -= fill.Quantity
								sells++
							}
							delete(pending, symbol)
						}
						if s.SubmittedOrder != nil {
							pending[symbol] = s.SubmittedOrder
						}
						if day == 5 && s.SubmittedOrder != nil {
							t.Fatal("final-bar submission")
						}
						if s.Signal.Action == "hold" && quantities[symbol] > 0 {
							holdingDays++
						}
						if zeroVolume && symbol == "AAPL" {
							if (s.Indicator.VWAP == nil) != (day < 2) || (s.Bar.VWAP == nil) != (day < 2 || day == 5) {
								t.Fatal("full-history null availability changed over HTTP")
							}
						}
						if symbol == "MSFT" && (s.Indicator.StandardDeviation == nil || *s.Indicator.StandardDeviation != 0) {
							t.Fatal("valid numeric zero lost over HTTP")
						}
					}
					index := 3 + day*3
					portfolio := decodeReplay[ReplayPortfolioData](t, events[index])
					if events[index].ID != index+1 || events[index].Type != ReplayPortfolio || portfolio.Date != date || portfolio.DayIndex != day || len(portfolio.Positions) != 2 {
						t.Fatal("daily portfolio ordering changed")
					}
					equity := cash
					for symbol, quantity := range quantities {
						position := portfolio.Positions[symbol]
						near(position.Quantity, quantity)
						equity += quantity * position.CurrentPrice
					}
					near(portfolio.Account.Cash, cash)
					near(portfolio.Account.Equity, equity)
					if day == 3 && !zeroVolume {
						previous := decodeReplay[ReplayPortfolioData](t, events[9])
						if portfolio.Account.Cash != previous.Account.Cash || portfolio.Account.Equity == previous.Account.Equity {
							t.Fatal("holding position did not update equity as price changed")
						}
					}
				}
				if !zeroVolume && (buys != 1 || sells != 1 || holdingDays < 2) {
					t.Fatalf("fixture lacks fills/holding: %d/%d/%d", buys, sells, holdingDays)
				}
				if events[0].Type != ReplayStart || events[19].Type != ReplayComplete || events[19].ID != 20 {
					t.Fatal("missing start or completion")
				}
				final := decodeReplay[ReplayCompleteData](t, events[19])
				near(final.FinalAccount.Cash, cash)
				near(final.FinalAccount.Equity, completed.FinalAccount.Equity)
			})
		}
	}
}

func TestPhase4ConcurrentReplayAdmissionAndRecovery(t *testing.T) {
	h, _, id := readyReplay(t)
	entered, done := make(chan struct{}, 3), make(chan struct{}, 3)
	h.waitReplay = func(ctx context.Context, _ time.Duration) error {
		entered <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r)
		if strings.HasSuffix(r.URL.Path, "/replay") {
			done <- struct{}{}
		}
	}))
	defer server.Close()
	client := server.Client()
	client.Timeout = 3 * time.Second
	path := server.URL + "/api/backtests/" + id
	streams := make([]*http.Response, 2)
	for i := range streams {
		response, err := client.Get(path + "/replay")
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != 200 {
			response.Body.Close()
			t.Fatal("stream admission failed")
		}
		streams[i] = response
		defer response.Body.Close()
		waitTestSignal(t, entered)
	}
	response, err := client.Get(path + "/replay")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 503 || response.Header.Get("Retry-After") == "" {
		t.Fatal("full replay budget did not reject admission")
	}
	waitTestSignal(t, done) // rejected request finished
	for _, suffix := range []string{"", "/result"} {
		response, err := client.Get(path + suffix)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatal("active streams blocked ordinary requests")
		}
	}
	streams[0].Body.Close()
	waitTestSignal(t, done)
	response, err = client.Get(path + "/replay")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 {
		response.Body.Close()
		t.Fatal("released replay slot was not reusable")
	}
	waitTestSignal(t, entered)
	response.Body.Close()
	streams[1].Body.Close()
	waitTestSignal(t, done)
	waitTestSignal(t, done)
	if len(h.replays) != 0 {
		t.Fatal("connections did not release all replay slots")
	}
}

func TestPhase4ThreeHourLifetimeAndRetention(t *testing.T) {
	h, m, id := readyReplay(t)
	// A result older than the former 30-minute default must still be retained.
	m.mu.Lock()
	m.jobs[id].finished = time.Now().Add(-2 * time.Hour)
	m.mu.Unlock()
	if _, err := m.Result(id); err != nil {
		t.Fatalf("result expired before the three-hour window: %v", err)
	}
	var previousDeadline time.Time
	for replay := 0; replay < 2; replay++ {
		ctx, cancel := context.WithCancel(context.Background())
		h.waitReplay = func(ctx context.Context, _ time.Duration) error {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) < 3*time.Hour-time.Minute || time.Until(deadline) > 3*time.Hour {
				t.Fatal("replay did not receive its own three-hour lifetime")
			}
			if !previousDeadline.IsZero() && !deadline.After(previousDeadline) {
				t.Fatal("second replay reused first replay's timer")
			}
			previousDeadline = deadline
			cancel()
			return ctx.Err()
		}
		w := newReplayRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/backtests/"+id+"/replay", nil).WithContext(ctx))
		cancel()
		if len(h.replays) != 0 {
			t.Fatal("replay slot leaked")
		}
	}
	m.mu.Lock()
	m.jobs[id].finished = time.Now().Add(-3*time.Hour - time.Second)
	m.mu.Unlock()
	if _, err := m.Result(id); !errors.Is(err, ErrNotFound) {
		t.Fatal("three-hour retention expiry not enforced")
	}
}
