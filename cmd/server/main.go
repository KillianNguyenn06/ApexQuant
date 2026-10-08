package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"apexquant/internal/api"
	"apexquant/internal/backtest"
	"apexquant/internal/marketdata"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	jobsOptions := api.DefaultJobOptions()
	httpOptions := api.DefaultHandlerOptions()
	settings := backtest.DefaultServiceSettings()
	flag.IntVar(&jobsOptions.Workers, "workers", jobsOptions.Workers, "maximum concurrent backtests")
	flag.IntVar(&jobsOptions.QueueSize, "queue-size", jobsOptions.QueueSize, "maximum waiting jobs")
	flag.IntVar(&jobsOptions.MaxRetained, "retained-jobs", jobsOptions.MaxRetained, "maximum finished job records")
	flag.DurationVar(&jobsOptions.Retention, "retention", jobsOptions.Retention, "finished job lifetime")
	flag.Int64Var(&jobsOptions.MaxResultBytes, "max-result-bytes", jobsOptions.MaxResultBytes, "maximum serialized bytes per result")
	flag.Int64Var(&jobsOptions.MaxStoredBytes, "max-stored-bytes", jobsOptions.MaxStoredBytes, "maximum retained result bytes")
	flag.Int64Var(&httpOptions.MaxBodyBytes, "max-body-bytes", httpOptions.MaxBodyBytes, "maximum request body bytes")
	flag.IntVar(&httpOptions.MaxRangeDays, "max-range-days", httpOptions.MaxRangeDays, "maximum requested calendar days")
	flag.IntVar(&httpOptions.MaxSnapshots, "max-snapshots", httpOptions.MaxSnapshots, "maximum calendar-days times symbols")
	flag.IntVar(&httpOptions.MaxInFlight, "max-inflight", httpOptions.MaxInFlight, "maximum simultaneous HTTP handlers")
	flag.IntVar(&httpOptions.RequestsPerMinute, "requests-per-minute", httpOptions.RequestsPerMinute, "shared request refill rate")
	flag.IntVar(&httpOptions.RequestBurst, "request-burst", httpOptions.RequestBurst, "shared request burst capacity")
	flag.IntVar(&httpOptions.SubmissionsPerMinute, "submissions-per-minute", httpOptions.SubmissionsPerMinute, "shared submission refill rate")
	flag.IntVar(&httpOptions.SubmissionBurst, "submission-burst", httpOptions.SubmissionBurst, "shared submission burst capacity")
	flag.IntVar(&httpOptions.Replay.MaxConnections, "replay-connections", httpOptions.Replay.MaxConnections, "maximum simultaneous replay streams")
	flag.Int64Var(&httpOptions.Replay.MaxBytes, "replay-max-bytes", httpOptions.Replay.MaxBytes, "maximum serialized source or timeline bytes per replay")
	flag.DurationVar(&httpOptions.Replay.WriteTimeout, "replay-write-timeout", httpOptions.Replay.WriteTimeout, "maximum time for each replay event write and flush")
	flag.DurationVar(&httpOptions.Replay.MaxDuration, "replay-max-duration", httpOptions.Replay.MaxDuration, "maximum lifetime per replay connection")
	flag.IntVar(&settings.MonteCarlo.NumPaths, "paths", settings.MonteCarlo.NumPaths, "Monte Carlo paths per evaluation")
	flag.IntVar(&settings.MonteCarlo.NumSteps, "steps", settings.MonteCarlo.NumSteps, "Monte Carlo steps per path")
	flag.Int64Var(&settings.MonteCarlo.Seed, "seed", settings.MonteCarlo.Seed, "simulation seed (zero is randomized)")
	flag.Parse()
	credentials := backtest.Credentials{AlpacaKey: os.Getenv("APCA_API_KEY_ID"), AlpacaSecret: os.Getenv("APCA_API_SECRET_KEY"), FREDKey: os.Getenv("FRED_API_KEY")}
	if credentials.AlpacaKey == "" || credentials.AlpacaSecret == "" || credentials.FREDKey == "" {
		log.Fatal("Export APCA_API_KEY_ID, APCA_API_SECRET_KEY, and FRED_API_KEY before starting the server.")
	}
	service, err := backtest.NewService(marketdata.NewClient(), credentials, settings)
	if err != nil {
		log.Fatal(err)
	}
	jobs, err := api.NewJobManager(api.ServiceRunner(service, api.DataInfo{
		Feed:       "sip",
		Timeframe:  "1Day",
		Adjustment: "raw"}), jobsOptions)
	if err != nil {
		log.Fatal(err)
	}
	handler, err := api.NewHandler(jobs, httpOptions)
	if err != nil {
		_ = jobs.Close(context.Background())
		log.Fatal(err)
	}
	server := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serving := make(chan error, 1)
	go func() { log.Printf("Backtest API listening on %s", *addr); serving <- server.ListenAndServe() }()
	select {
	case <-ctx.Done():
	case err := <-serving:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP server stopped: %v", err)
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Close cancels queued/provider work. A running engine may need to finish;
	// process exit after this grace period clears all in-memory jobs.
	closed := make(chan error, 1)
	go func() { closed <- jobs.Close(shutdown) }()
	if err := server.Shutdown(shutdown); err != nil {
		log.Printf("HTTP shutdown: %v", err)
		_ = server.Close()
	}
	if err := <-closed; err != nil {
		log.Printf("Job shutdown: %v (engine cancellation is not yet supported)", err)
	}
}
