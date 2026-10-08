package api

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"

	"apexquant/internal/backtest"
)

func replayFixture(t *testing.T) (BacktestResultResponse, backtest.RunOutput) {
	t.Helper()
	service, _ := tradingService(t, tradingProvider{})
	validated, err := ValidateCreateBacktestRequest(validRequest(), testNow())
	if err != nil {
		t.Fatal(err)
	}
	output, err := service.Run(context.Background(), backtest.RunRequest{InitialCapital: validated.Request.InitialCapital, Allocations: validated.Allocations, Start: validated.Start, End: validated.End})
	if err != nil {
		t.Fatal(err)
	}
	response, err := NewResultResponse("replay-job", validated.Request, output.Config, DataInfo{Feed: "sip", Timeframe: "1Day", Adjustment: "raw"}, output.Result)
	if err != nil {
		t.Fatal(err)
	}
	return response, output
}

func decodeReplay[T any](t *testing.T, event ReplayEvent) T {
	t.Helper()
	var data T
	if err := json.Unmarshal(event.Data, &data); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestReplaySequenceAndValues(t *testing.T) {
	response, _ := replayFixture(t)
	timeline, err := NewReplayTimeline(response)
	if err != nil {
		t.Fatal(err)
	}
	// This expected sequence is independent of the timeline builder.
	want := []struct {
		kind         ReplayEventType
		date, symbol string
	}{
		{ReplayStart, "", ""},
		{ReplaySnapshot, "2025-09-05", "AAPL"}, {ReplaySnapshot, "2025-09-05", "MSFT"}, {ReplayPortfolio, "2025-09-05", ""},
		{ReplaySnapshot, "2025-09-06", "AAPL"}, {ReplaySnapshot, "2025-09-06", "MSFT"}, {ReplayPortfolio, "2025-09-06", ""},
		{ReplaySnapshot, "2025-09-07", "AAPL"}, {ReplaySnapshot, "2025-09-07", "MSFT"}, {ReplayPortfolio, "2025-09-07", ""},
		{ReplaySnapshot, "2025-09-08", "AAPL"}, {ReplaySnapshot, "2025-09-08", "MSFT"}, {ReplayPortfolio, "2025-09-08", ""},
		{ReplayComplete, "", ""},
	}
	if timeline.Len() != len(want) || timeline.Days() != 4 {
		t.Fatalf("events=%d days=%d", timeline.Len(), timeline.Days())
	}
	snapshotIndex, dayIndex, holds, fills, submissions := 0, 0, 0, 0, 0
	for i, expected := range want {
		event, ok := timeline.Event(i)
		if !ok || event.ID != i+1 || event.Type != expected.kind {
			t.Fatalf("event %d = %+v", i, event)
		}
		switch event.Type {
		case ReplayStart:
			start := decodeReplay[ReplayStartData](t, event)
			if start.JobID != response.ID || start.TotalDays != 4 || start.TotalSnapshots != 8 || !reflect.DeepEqual(start.Request, response.Request) || !reflect.DeepEqual(start.Settings, response.Settings) || !reflect.DeepEqual(start.Data, response.Data) {
				t.Fatal("incorrect start metadata")
			}
		case ReplaySnapshot:
			data := decodeReplay[ReplaySnapshotData](t, event)
			if data.DayIndex != dayIndex || data.SnapshotIndex != snapshotIndex || data.Snapshot.Bar.Symbol != expected.symbol || data.Snapshot.Timestamp.UTC().Format(time.DateOnly) != expected.date || !reflect.DeepEqual(data.Snapshot, response.Snapshots[snapshotIndex]) {
				t.Fatalf("snapshot %d differs from expected order or completed result", snapshotIndex)
			}
			if data.Snapshot.Signal.Action == "hold" {
				holds++
			}
			if data.Snapshot.FilledOrder != nil {
				fills++
			}
			if data.Snapshot.SubmittedOrder != nil {
				submissions++
			}
			snapshotIndex++
		case ReplayPortfolio:
			data := decodeReplay[ReplayPortfolioData](t, event)
			if data.Date != expected.date || data.DayIndex != dayIndex || data.Account != response.Snapshots[dayIndex*2].Account || len(data.Positions) != 2 {
				t.Fatal("incoherent daily portfolio update")
			}
			for _, s := range response.Snapshots[dayIndex*2 : (dayIndex+1)*2] {
				if !reflect.DeepEqual(data.Positions[s.Bar.Symbol], s.Position) {
					t.Fatal("daily position differs from completed result")
				}
			}
			dayIndex++
		case ReplayComplete:
			data := decodeReplay[ReplayCompleteData](t, event)
			if data.JobID != response.ID || data.TotalDays != 4 || data.TotalSnapshots != 8 || data.FinalAccount != response.FinalAccount || !reflect.DeepEqual(data.FinalPositions, response.FinalPositions) {
				t.Fatal("incorrect completion values")
			}
		}
	}
	if holds == 0 || fills != 2 || submissions != 2 {
		t.Fatalf("fixture did not exercise Hold/submissions/fills: %d/%d/%d", holds, submissions, fills)
	}
	for _, i := range []int{-1, timeline.Len()} {
		if _, ok := timeline.Event(i); ok {
			t.Fatal("out-of-range event accepted")
		}
	}
}

func TestReplayPreservesFullHistoryAvailability(t *testing.T) {
	response, output := replayFixture(t)
	bars := output.Config.BarsBySymbol["AAPL"]
	for _, i := range []int{0, 1, 3} {
		bars[i].Volume, bars[i].VWAP = 0, 0
	}
	result, err := backtest.RunBacktest(output.Config)
	if err != nil {
		t.Fatal(err)
	}
	response, err = NewResultResponse(response.ID, response.Request, output.Config, response.Data, result)
	if err != nil {
		t.Fatal(err)
	}
	timeline, err := NewReplayTimeline(response)
	if err != nil {
		t.Fatal(err)
	}
	for day := 0; day < 4; day++ {
		event, _ := timeline.Event(1 + day*3)
		s := decodeReplay[ReplaySnapshotData](t, event).Snapshot
		if (s.Indicator.VWAP == nil) != (day < 2) {
			t.Fatal("lost cumulative volume availability")
		}
		if (s.Bar.VWAP == nil) != (day != 2) {
			t.Fatal("lost zero-volume bar null")
		}
		event, _ = timeline.Event(2 + day*3)
		other := decodeReplay[ReplaySnapshotData](t, event).Snapshot
		if other.Indicator.VWAP == nil {
			t.Fatal("another stock's null state leaked")
		}
		if day == 0 && (other.Indicator.StandardDeviation == nil || *other.Indicator.StandardDeviation != 0) {
			t.Fatal("valid zero became null")
		}
	}
}

func TestReplayOwnsEncodedEvents(t *testing.T) {
	response, _ := replayFixture(t)
	timeline, err := NewReplayTimeline(response)
	if err != nil {
		t.Fatal(err)
	}
	want := make([]ReplayEvent, timeline.Len())
	for i := range want {
		want[i], _ = timeline.Event(i)
	}
	response.Request.Allocations[0].Symbol = "CHANGED"
	*response.Snapshots[0].Indicator.VWAP = -1
	response.Snapshots[0].Account.Cash = -1
	response.FinalPositions["OTHER"] = Position{Symbol: "OTHER"}
	for i := range want {
		got, _ := timeline.Event(i)
		if !reflect.DeepEqual(got, want[i]) {
			t.Fatal("input mutation changed timeline")
		}
		got.Data[0] = '!'
		again, _ := timeline.Event(i)
		if !reflect.DeepEqual(again, want[i]) {
			t.Fatal("reader mutation changed timeline")
		}
	}
}

func TestReplayRejectsInconsistentResults(t *testing.T) {
	for name, change := range map[string]func(*BacktestResultResponse){
		"empty":            func(r *BacktestResultResponse) { r.Snapshots = nil },
		"incomplete day":   func(r *BacktestResultResponse) { r.Snapshots = r.Snapshots[:7] },
		"duplicate symbol": func(r *BacktestResultResponse) { r.Request.Allocations[1].Symbol = "AAPL" },
		"symbol order":     func(r *BacktestResultResponse) { r.Snapshots[0], r.Snapshots[1] = r.Snapshots[1], r.Snapshots[0] },
		"repeated day":     func(r *BacktestResultResponse) { copy(r.Snapshots[2:4], r.Snapshots[:2]) },
		"backward day": func(r *BacktestResultResponse) {
			r.Snapshots = append(append([]Snapshot{}, r.Snapshots[2:4]...), append(r.Snapshots[:2], r.Snapshots[4:]...)...)
		},
		"mismatched date": func(r *BacktestResultResponse) {
			r.Snapshots[1].Timestamp = r.Snapshots[1].Timestamp.AddDate(0, 0, 1)
			r.Snapshots[1].Bar.Timestamp = r.Snapshots[1].Timestamp
		},
		"zero timestamp": func(r *BacktestResultResponse) { r.Snapshots[0].Timestamp = time.Time{} },
		"bar timestamp": func(r *BacktestResultResponse) {
			r.Snapshots[0].Bar.Timestamp = r.Snapshots[0].Bar.Timestamp.Add(time.Hour)
		},
		"daily account":      func(r *BacktestResultResponse) { r.Snapshots[1].Account.Cash++ },
		"final account":      func(r *BacktestResultResponse) { r.FinalAccount.Equity++ },
		"final positions":    func(r *BacktestResultResponse) { delete(r.FinalPositions, "MSFT") },
		"nonfinite snapshot": func(r *BacktestResultResponse) { r.Snapshots[0].Bar.Close = math.NaN() },
		"nonfinite metadata": func(r *BacktestResultResponse) { r.Settings.InitialVolatility = math.Inf(1) },
	} {
		t.Run(name, func(t *testing.T) {
			response, _ := replayFixture(t)
			change(&response)
			timeline, err := NewReplayTimeline(response)
			if err == nil || timeline != nil {
				t.Fatal("inconsistent result silently accepted")
			}
		})
	}
}

func TestReplayUsesDailyOrderRatherThanSortingTimestamps(t *testing.T) {
	response, _ := replayFixture(t)
	for i := range response.Snapshots {
		s := &response.Snapshots[i]
		if s.Bar.Symbol == "AAPL" {
			s.Timestamp = s.Timestamp.Add(2 * time.Hour)
			s.Bar.Timestamp = s.Timestamp
		}
	}
	if !response.Snapshots[0].Timestamp.After(response.Snapshots[1].Timestamp) {
		t.Fatal("fixture does not exercise different intraday times")
	}
	timeline, err := NewReplayTimeline(response)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range []struct {
		index  int
		symbol string
	}{{1, "AAPL"}, {2, "MSFT"}} {
		event, _ := timeline.Event(pair.index)
		if decodeReplay[ReplaySnapshotData](t, event).Snapshot.Bar.Symbol != pair.symbol {
			t.Fatal("allocation order changed by timestamp sorting")
		}
	}
}

func TestReplayDayInterval(t *testing.T) {
	for _, tc := range []struct {
		speed    float64
		interval time.Duration
	}{{.25, 4 * time.Second}, {1, time.Second}, {2, 500 * time.Millisecond}, {20, 50 * time.Millisecond}} {
		got, err := ReplayDayInterval(tc.speed)
		if err != nil || got != tc.interval {
			t.Fatalf("speed=%v interval=%v err=%v", tc.speed, got, err)
		}
	}
	for _, speed := range []float64{0, -1, .24, 20.01, math.NaN(), math.Inf(1)} {
		if _, err := ReplayDayInterval(speed); err == nil {
			t.Fatalf("accepted speed %v", speed)
		}
	}
}
