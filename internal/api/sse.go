package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type ReplayOptions struct {
	MaxConnections int
	MaxBytes       int64
	WriteTimeout   time.Duration
	MaxDuration    time.Duration
}

func DefaultReplayOptions() ReplayOptions {
	return ReplayOptions{
		MaxConnections: 2,
		MaxBytes:       32 << 20,
		WriteTimeout:   5 * time.Second,
		MaxDuration:    3 * time.Hour,
	}
}

func (o ReplayOptions) valid() bool {
	return o.MaxConnections > 0 && o.MaxBytes > 0 && o.MaxBytes <= 1<<30 && o.WriteTimeout > 0 && o.MaxDuration > 0
}

func parseReplayRequest(r *http.Request) (time.Duration, int, error) {
	invalid := func() (time.Duration, int, error) {
		return 0, 0, &APIError{
			Code:    "invalid_replay_request",
			Message: "Use one optional speed and a nonnegative Last-Event-ID.",
		}
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return invalid()
	}
	for key, values := range query {
		if key != "speed" || len(values) != 1 {
			return invalid()
		}
	}
	speed := 1.0
	if values, ok := query["speed"]; ok {
		speed, err = strconv.ParseFloat(values[0], 64)
		if err != nil {
			return invalid()
		}
	}
	interval, err := ReplayDayInterval(speed)
	if err != nil {
		return 0, 0, err
	}
	cursor := 0
	if values := r.Header.Values("Last-Event-ID"); len(values) > 0 {
		if len(values) != 1 || values[0] == "" {
			return invalid()
		}
		for _, digit := range values[0] {
			if digit < '0' || digit > '9' {
				return invalid()
			}
		}
		cursor, err = strconv.Atoi(values[0])
		if err != nil {
			return invalid()
		}
	}
	return interval, cursor, nil
}

func (h *Handler) replay(w http.ResponseWriter, r *http.Request, id string) {
	interval, cursor, err := parseReplayRequest(r)
	if err != nil {
		writeAPIError(w, 400, err)
		return
	}
	if h.jobs.ctx.Err() != nil {
		writeError(w, 503, "server_stopping", "Server is stopping.")
		return
	}
	select {
	case h.replays <- struct{}{}:
		defer func() { <-h.replays }()
	default:
		w.Header().Set("Retry-After", "5")
		writeError(w, 503, "replay_capacity", "Too many active replay connections.")
		return
	}
	// Cancellation belongs to this replay, never to the completed simulation.
	ctx, cancel := context.WithTimeout(r.Context(), h.options.Replay.MaxDuration)
	defer cancel()
	stop := context.AfterFunc(h.jobs.ctx, cancel)
	defer stop()
	if ctx.Err() != nil {
		return
	}
	timeline, symbolCount, err := h.loadReplay(id)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			writeError(w, 404, "job_not_found", "Backtest not found or expired.")
		case errors.Is(err, ErrNotReady):
			writeError(w, 409, "result_not_ready", "Backtest has not completed successfully.")
		case errors.Is(err, ErrResultTooLarge):
			writeError(w, 413, "replay_too_large", "Replay exceeds the configured byte budget.")
		default:
			var e *APIError
			if errors.As(err, &e) && e.Code == "replay_too_large" {
				writeAPIError(w, 413, err)
			} else {
				writeError(w, 500, "invalid_result", "Backtest result cannot be replayed safely.")
			}
		}
		return
	}
	if cursor > timeline.Len() {
		writeError(w, 400, "invalid_replay_cursor", "Last-Event-ID exceeds the replay's final event.")
		return
	}
	if ctx.Err() != nil {
		return
	}
	// EventSource stops reconnecting on 204. Completion also requires clients to
	// close their EventSource when the complete event is received.
	if cursor == timeline.Len() {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if _, ok := w.(http.Flusher); !ok {
		writeError(w, 500, "stream_unsupported", "Response does not support streaming.")
		return
	}
	controller := http.NewResponseController(w)
	if err := controller.SetWriteDeadline(time.Now().Add(h.options.Replay.WriteTimeout)); err != nil {
		writeError(w, 500, "stream_unsupported", "Response does not support bounded streaming.")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	for i := cursor; i < timeline.Len(); i++ {
		event := timeline.events[i]
		// Each day contains symbolCount snapshots followed by one portfolio event.
		// Resume starts immediately, including when the cursor is mid-day.
		if i > cursor && event.Type == ReplaySnapshot && i > 1 && (i-1)%(symbolCount+1) == 0 {
			// Playback idle time is exempt from the global write timeout. Keep a
			// finite deadline for writes and net/http's response finalization.
			if err := controller.SetWriteDeadline(time.Time{}); err != nil {
				return
			}
			if err := h.waitReplay(ctx, interval); err != nil {
				_ = controller.SetWriteDeadline(time.Now().Add(h.options.Replay.WriteTimeout))
				return
			}
		}
		if ctx.Err() != nil {
			_ = controller.SetWriteDeadline(time.Now().Add(h.options.Replay.WriteTimeout))
			return
		}
		if err := controller.SetWriteDeadline(time.Now().Add(h.options.Replay.WriteTimeout)); err != nil {
			return
		}
		if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.ID, event.Type, event.Data); err != nil {
			return
		}
		if err := controller.Flush(); err != nil {
			return
		}
	}
}

func (h *Handler) loadReplay(id string) (*ReplayTimeline, int, error) {
	encoded, err := h.jobs.resultLimited(id, h.options.Replay.MaxBytes)
	if err != nil {
		return nil, 0, err
	}
	var response BacktestResultResponse
	if err := json.Unmarshal(encoded, &response); err != nil {
		return nil, 0, err
	}
	if response.ID != id {
		return nil, 0, errors.New("replay job identity mismatch")
	}
	if len(response.Snapshots) > h.options.MaxSnapshots {
		return nil, 0, &APIError{Code: "replay_too_large", Message: "Replay exceeds the configured snapshot budget."}
	}
	timeline, err := newReplayTimeline(response, h.options.Replay.MaxBytes)
	return timeline, len(response.Request.Allocations), err
}

func waitReplayDay(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}
