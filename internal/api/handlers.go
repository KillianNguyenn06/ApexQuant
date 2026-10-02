package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"

	"apexquant/internal/backtest"
)

type HandlerOptions struct {
	MaxBodyBytes                                                           int64
	MaxRangeDays, MaxSnapshots, MaxInFlight                                int
	RequestsPerMinute, RequestBurst, SubmissionsPerMinute, SubmissionBurst int
}

func DefaultHandlerOptions() HandlerOptions {
	return HandlerOptions{
		MaxBodyBytes:         16 << 10,
		MaxRangeDays:         366,
		MaxSnapshots:         8 * 366,
		MaxInFlight:          16,
		RequestsPerMinute:    120,
		RequestBurst:         30,
		SubmissionsPerMinute: 6,
		SubmissionBurst:      2,
	}
}

// A shared token bucket has constant memory usage. This local application's
// clients share one budget; no unbounded per-IP map or trusted proxy headers.
type bucket struct {
	mu                          sync.Mutex
	tokens, capacity, perSecond float64
	last                        time.Time
}

func newBucket(perMinute, burst int) *bucket {
	return &bucket{
		tokens:    float64(burst),
		capacity:  float64(burst),
		perSecond: float64(perMinute) / 60,
		last:      time.Now(),
	}
}
func (b *bucket) allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Concurrent callers may capture timestamps before acquiring this lock.
	// Never move the refill clock backward and credit the same interval twice.
	if now.After(b.last) {
		b.tokens = min(b.capacity, b.tokens+now.Sub(b.last).Seconds()*b.perSecond)
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

type Handler struct {
	jobs                  *JobManager
	options               HandlerOptions
	requests, submissions *bucket
	inflight              chan struct{}
}

func NewHandler(jobs *JobManager, options HandlerOptions) (*Handler, error) {
	if jobs == nil || options.MaxBodyBytes < 1 || options.MaxBodyBytes > 1<<30 || options.MaxRangeDays < 2 || options.MaxSnapshots < 2 || options.MaxInFlight < 1 || options.RequestsPerMinute < 1 || options.RequestBurst < 1 || options.SubmissionsPerMinute < 1 || options.SubmissionBurst < 1 {
		return nil, fmt.Errorf("invalid HTTP limits")
	}
	return &Handler{
		jobs:        jobs,
		options:     options,
		requests:    newBucket(options.RequestsPerMinute, options.RequestBurst),
		submissions: newBucket(options.SubmissionsPerMinute, options.SubmissionBurst),
		inflight:    make(chan struct{}, options.MaxInFlight)}, nil
}
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if !h.requests.allow(time.Now()) {
		w.Header().Set("Retry-After", "60")
		writeError(w, 429, "rate_limited", "Request rate limit exceeded.")
		return
	}
	select {
	case h.inflight <- struct{}{}:
		defer func() { <-h.inflight }()
	default:
		writeError(w, 503, "server_busy", "Too many simultaneous requests.")
		return
	}
	if r.URL.Path == "/api/backtests" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeError(w, 405, "method_not_allowed", "Use POST for this endpoint.")
			return
		}
		h.create(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/backtests/") {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/backtests/"), "/")
		if parts[0] != "" && (len(parts) == 1 || (len(parts) == 2 && parts[1] == "result")) {
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", "GET")
				writeError(w, 405, "method_not_allowed", "Use GET for this endpoint.")
				return
			}
			if len(parts) == 2 {
				h.result(w, parts[0])
			} else {
				h.status(w, parts[0])
			}
			return
		}
	}
	writeError(w, 404, "not_found", "Endpoint not found.")
}
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	if !h.submissions.allow(time.Now()) {
		w.Header().Set("Retry-After", "60")
		writeError(w, 429, "rate_limited", "Backtest submission rate limit exceeded.")
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeError(w, 415, "unsupported_media_type", "Use application/json.")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, h.options.MaxBodyBytes+1))
	if int64(len(body)) > h.options.MaxBodyBytes {
		writeError(w, 413, "request_too_large", "Request body exceeds the configured limit.")
		return
	}
	if err != nil {
		writeError(w, 400, "invalid_json", "Request body could not be read.")
		return
	}
	request, err := DecodeCreateBacktestRequest(bytes.NewReader(body))
	if err != nil {
		writeAPIError(w, 400, err)
		return
	}
	validated, err := ValidateCreateBacktestRequest(request, time.Now())
	if err != nil {
		status := 400
		var e *APIError
		if errors.As(err, &e) && e.Code == "internal_error" {
			status = 500
		}
		writeAPIError(w, status, err)
		return
	}
	if err := ValidateRequestLimits(validated, h.options.MaxRangeDays, h.options.MaxSnapshots); err != nil {
		writeAPIError(w, 400, err)
		return
	}
	if r.Context().Err() != nil {
		return
	}
	accepted, err := h.jobs.Submit(validated)
	if err != nil {
		w.Header().Set("Retry-After", "5")
		writeError(w, 503, "capacity_unavailable", "Backtest queue is full or the server is stopping.")
		return
	}
	w.Header().Set("Location", "/api/backtests/"+accepted.ID)
	writeJSON(w, 202, accepted)
}
func (h *Handler) status(w http.ResponseWriter, id string) {
	out, err := h.jobs.Status(id)
	if err != nil {
		writeError(w, 404, "job_not_found", "Backtest not found or expired.")
		return
	}
	writeJSON(w, 200, out)
}
func (h *Handler) result(w http.ResponseWriter, id string) {
	data, err := h.jobs.Result(id)
	if errors.Is(err, ErrNotFound) {
		writeError(w, 404, "job_not_found", "Backtest not found or expired.")
		return
	}
	if err != nil {
		writeError(w, 409, "result_not_ready", "Backtest has not completed successfully.")
		return
	}
	w.WriteHeader(200)
	_, _ = w.Write(data)
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		writeError(w, 500, "internal_error", "Response could not be encoded.")
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, ErrorResponse{
		Error: &APIError{Code: code, Message: message},
	})
}
func writeAPIError(w http.ResponseWriter, status int, err error) {
	var e *APIError
	if !errors.As(err, &e) {
		writeError(w, 500, "internal_error", "Request could not be processed.")
		return
	}
	writeJSON(w, status, ErrorResponse{Error: e})
}

// ServiceRunner connects API DTOs to the provider-independent service.
// Metadata is supplied by the server composition root to match its configured provider.
func ServiceRunner(service *backtest.Service, metadata DataInfo) JobRunner {
	return func(ctx context.Context, id string, request ValidatedRequest) (BacktestResultResponse, error) {
		out, err := service.Run(ctx, backtest.RunRequest{
			InitialCapital: request.Request.InitialCapital,
			Allocations:    request.Allocations,
			Start:          request.Start,
			End:            request.End,
		})
		if err != nil {
			var e *backtest.ServiceError
			if errors.As(err, &e) {
				return BacktestResultResponse{}, &APIError{Code: e.Code}
			}
			return BacktestResultResponse{}, err
		}
		data := metadata
		data.RiskFreeRate = out.Config.MonteCarloInput.RiskFreeRate
		return NewResultResponse(id, request.Request, out.Config, data, out.Result)
	}
}
