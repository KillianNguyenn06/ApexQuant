package api

import (
	"encoding/json"
	"math"
	"reflect"
	"time"
)

type ReplayEventType string

const (
	ReplayStart     ReplayEventType = "start"
	ReplaySnapshot  ReplayEventType = "snapshot"
	ReplayPortfolio ReplayEventType = "portfolio"
	ReplayComplete  ReplayEventType = "complete"
)

// ID is a one-based sequence within one completed job's replay.
// Data is already encoded JSON, ready for transport; event order is independent of wall time.
type ReplayEvent struct {
	ID   int             `json:"id"`
	Type ReplayEventType `json:"type"`
	Data json.RawMessage `json:"data"`
}

type ReplayStartData struct {
	JobID          string                `json:"job_id"`
	Request        CreateBacktestRequest `json:"request"`
	Settings       SimulationSettings    `json:"settings"`
	Data           DataInfo              `json:"data"`
	TotalDays      int                   `json:"total_days"`
	TotalSnapshots int                   `json:"total_snapshots"`
}

type ReplaySnapshotData struct {
	DayIndex      int      `json:"day_index"`
	SnapshotIndex int      `json:"snapshot_index"`
	Snapshot      Snapshot `json:"snapshot"`
}

type ReplayPortfolioData struct {
	DayIndex  int                 `json:"day_index"`
	Date      string              `json:"date"`
	Account   Account             `json:"account"`
	Positions map[string]Position `json:"positions"`
}

type ReplayCompleteData struct {
	JobID          string              `json:"job_id"`
	TotalDays      int                 `json:"total_days"`
	TotalSnapshots int                 `json:"total_snapshots"`
	FinalAccount   Account             `json:"final_account"`
	FinalPositions map[string]Position `json:"final_positions"`
}

// ReplayTimeline owns immutable encoded events.
// No engine execution, sleeping, HTTP, or job retention is performed here. Event returns a copy for each reader.
type ReplayTimeline struct {
	events []ReplayEvent
	days   int
}

func (r *ReplayTimeline) Len() int  { return len(r.events) }
func (r *ReplayTimeline) Days() int { return r.days }
func (r *ReplayTimeline) Event(index int) (ReplayEvent, bool) {
	if index < 0 || index >= len(r.events) {
		return ReplayEvent{}, false
	}
	event := r.events[index]
	event.Data = append(json.RawMessage(nil), event.Data...)
	return event, true
}

// NewReplayTimeline accepts the COMPLETE mapped response so volume-dependent
// null availability is retained. It rejects inconsistent order instead of
// sorting, dropping, or remapping snapshots. The engine aligns UTC calendar
// dates, not exact timestamps: within a day allocation order takes precedence.
func NewReplayTimeline(result BacktestResultResponse) (*ReplayTimeline, error) {
	return newReplayTimeline(result, 0)
}

// maxBytes bounds encoded events (including a conservative framing allowance).
// The transport additionally bounds source bytes and snapshot count before this.
func newReplayTimeline(result BacktestResultResponse, maxBytes int64) (*ReplayTimeline, error) {
	invalid := func() (*ReplayTimeline, error) {
		return nil, &APIError{Code: "invalid_result", Message: "Backtest result is not a consistent replay timeline."}
	}
	n := len(result.Request.Allocations)
	if result.ID == "" || n < 1 || n > 8 || len(result.Snapshots) == 0 || len(result.Snapshots)%n != 0 {
		return invalid()
	}
	seen := make(map[string]bool, n)
	for _, a := range result.Request.Allocations {
		if a.Symbol == "" || seen[a.Symbol] {
			return invalid()
		}
		seen[a.Symbol] = true
	}
	r := &ReplayTimeline{days: len(result.Snapshots) / n}
	var encodedBytes int64
	appendEvent := func(kind ReplayEventType, data any) error {
		encoded, err := json.Marshal(data)
		if err != nil {
			return &APIError{Code: "invalid_result", Message: "Backtest result cannot be encoded for replay."}
		}
		encodedBytes += int64(len(encoded) + len(kind) + 64)
		if maxBytes > 0 && encodedBytes > maxBytes {
			return &APIError{Code: "replay_too_large", Message: "Replay exceeds the configured byte budget."}
		}
		r.events = append(r.events, ReplayEvent{ID: len(r.events) + 1, Type: kind, Data: encoded})
		return nil
	}
	if err := appendEvent(ReplayStart, ReplayStartData{JobID: result.ID, Request: result.Request, Settings: result.Settings, Data: result.Data, TotalDays: r.days, TotalSnapshots: len(result.Snapshots)}); err != nil {
		return nil, err
	}
	previousDate := ""
	var finalPositions map[string]Position
	var finalAccount Account
	for day := 0; day < r.days; day++ {
		group := result.Snapshots[day*n : (day+1)*n]
		date := group[0].Timestamp.UTC().Format(time.DateOnly)
		if date <= previousDate {
			return invalid()
		}
		previousDate = date
		positions := make(map[string]Position, n)
		for symbolIndex, snapshot := range group {
			symbol := result.Request.Allocations[symbolIndex].Symbol
			if snapshot.Timestamp.IsZero() || !snapshot.Timestamp.Equal(snapshot.Bar.Timestamp) || snapshot.Timestamp.UTC().Format(time.DateOnly) != date || snapshot.Bar.Symbol != symbol || snapshot.Position.Symbol != symbol || snapshot.Signal.Symbol != symbol || snapshot.Account != group[0].Account {
				return invalid()
			}
			if err := appendEvent(ReplaySnapshot, ReplaySnapshotData{DayIndex: day, SnapshotIndex: day*n + symbolIndex, Snapshot: snapshot}); err != nil {
				return nil, err
			}
			positions[symbol] = snapshot.Position
		}
		if err := appendEvent(ReplayPortfolio, ReplayPortfolioData{DayIndex: day, Date: date, Account: group[0].Account, Positions: positions}); err != nil {
			return nil, err
		}
		finalAccount, finalPositions = group[0].Account, positions
	}
	if finalAccount != result.FinalAccount || !reflect.DeepEqual(finalPositions, result.FinalPositions) {
		return invalid()
	}
	if err := appendEvent(ReplayComplete, ReplayCompleteData{JobID: result.ID, TotalDays: r.days, TotalSnapshots: len(result.Snapshots), FinalAccount: result.FinalAccount, FinalPositions: result.FinalPositions}); err != nil {
		return nil, err
	}
	return r, nil
}

// ReplayDayInterval defines playback speed in historical days per real second.
// The first day is immediate; transport waits only BETWEEN daily groups. Start,
// a day's stock/portfolio events, and final completion carry no extra delay.
func ReplayDayInterval(daysPerSecond float64) (time.Duration, error) {
	if math.IsNaN(daysPerSecond) || math.IsInf(daysPerSecond, 0) || daysPerSecond < .25 || daysPerSecond > 20 {
		return 0, &APIError{Code: "invalid_replay_speed", Message: "Replay speed must be between 0.25 and 20 historical days per second.", Field: "speed"}
	}
	return time.Duration(float64(time.Second) / daysPerSecond), nil
}
