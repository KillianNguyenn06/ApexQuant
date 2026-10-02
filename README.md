# ApexQuant

ApexQuant is a Go-based multi-stock portfolio backtesting application.

Users select between 1 and 8 stock symbols, assign each stock a percentage of the portfolio, and run a one-year historical backtest using daily market data.

The application calculates indicators, generates trading signals, sizes positions using Monte Carlo simulation and Kelly-based risk management, and tracks orders, positions, cash, buying power, and portfolio equity.

> ApexQuant is currently under active development and is intended for educational and research purposes.

## Current Features

- User-selected portfolio of 1–8 stocks.
- Portfolio allocations totaling 100%.
- One year of daily historical bars from Alpaca.
- Risk-free rate from FRED.
- Independent state for every stock.
- Volume-weighted average price (VWAP).
- VWAP standard-deviation bands.
- Buy, Sell, and Hold signals.
- Annualized historical volatility.
- Geometric Brownian Motion Monte Carlo simulation.
- Kelly-based position sizing.
- Cash, buying-power, and allocation limits.
- Orders submitted at the current close.
- Pending orders filled at the next available open.
- Portfolio-wide equity calculation.
- Submitted and filled-order snapshots.
- Unit tests for algorithms, risk, simulation, broker, and session logic.

## Backtest Flow

For each historical trading day, ApexQuant performs the following sequence:

```text
1. Update every stock to its opening price
2. Calculate opening portfolio equity
3. Fill orders submitted on the previous bar
4. Update every stock to its closing price
5. Calculate closing portfolio equity
6. Update VWAP and standard-deviation bands
7. Generate Buy, Sell, or Hold signals
8. Calculate Monte Carlo probabilities
9. Apply Kelly and portfolio risk limits
10. Submit new orders
11. Save a backtest snapshot
```

The final historical bar may fill an existing pending order, but it cannot submit a new order because no future bar exists to fill it.

## Trading Strategy

### Buy Signal

A Buy signal is generated when:

```text
No position is currently open
Standard deviation is greater than zero
Current price is at or below the VWAP lower band
```

The initial risk levels are:

```text
Entry       = current price
Stop loss   = entry price - standard deviation
Take profit = VWAP + 0.5 × standard deviation
```

### Sell Signal

A Sell signal is generated when an open position reaches either:

```text
Current price <= stop-loss price
Current price >= take-profit price
```

### Position Sizing

The final order quantity is the smallest quantity allowed by:

```text
Kelly risk sizing
Available cash
Available buying power
Remaining allocation for that stock
```

Portfolio equity is calculated as:

```text
Equity = Cash + sum(position quantity × current market price)
```

## Project Structure

```text
ApexQuant/
├── cmd/
│   └── server/
│       └── main.go
├── internal/
│   ├── account/
│   │   └── models.go
│   ├── algorithm/
│   │   ├── models.go
│   │   └── models_test.go
│   ├── broker/
│   │   ├── order.go
│   │   └── order_test.go
│   ├── backtest/
│   │   ├── engine.go
│   │   ├── models.go
│   │   ├── validation.go
│   │   └── live_test.go
│   ├── marketdata/
│   │   └── models.go
│   ├── mockdata/
│   │   └── mock.go
│   ├── risk/
│   │   ├── risk.go
│   │   └── risk_test.go
│   ├── session/
│   │   ├── models.go
│   │   ├── models_test.go
│   │   └── snapshot.go
│   └── simulation/
│       ├── models.go
│       └── models_test.go
├── .gitignore
├── go.mod
├── go.sum
└── README.md
```

## Requirements

- Go 1.26.5 or compatible version.
- Alpaca Market Data API credentials.
- FRED API key.
- Internet access for historical market data and interest-rate data.

## Environment Variables

ApexQuant expects these environment variables:

```text
APCA_API_KEY_ID
APCA_API_SECRET_KEY
FRED_API_KEY
```

Example:

```bash
export APCA_API_KEY_ID="your-alpaca-key"
export APCA_API_SECRET_KEY="your-alpaca-secret"
export FRED_API_KEY="your-fred-key"
```

The `.env` file is excluded by `.gitignore`.

Go does not automatically load `.env` files in the current application, so the variables must be exported before running the program.

Never commit API credentials to Git.

## Installation

Clone the repository:

```bash
git clone git@github.com:KillianNguyenn06/ApexQuant.git
cd ApexQuant
```

Download dependencies:

```bash
go mod download
```

## Running the Backtest

Run:

```bash
go run ./cmd/backtest
```

The terminal will ask for:

1. The number of stocks.
2. Each ticker symbol.
3. The allocation percentage for each stock.

Example:

```text
How many stocks (1-8): 4

Stock ticker #1: AAPL
AAPL allocation percentage: 30

Stock ticker #2: MSFT
MSFT allocation percentage: 30

Stock ticker #3: NVDA
NVDA allocation percentage: 30

Stock ticker #4: AMD
AMD allocation percentage: 10
```

The allocations must total exactly 100%.

After the backtest completes, the terminal displays stored snapshots:

- Trading date.
- Symbol.
- Closing price.
- VWAP.
- Lower and upper bands.
- Buy, Sell, or Hold signal.
- Submitted orders.
- Filled orders.
- Cash.
- Buying power.
- Portfolio equity.

## Running Tests

Run the complete test suite:

```bash
go test ./...
```

Run tests with detailed output:

```bash
go test ./... -v
```

Run static analysis:

```bash
go vet ./...
```

Run tests with the race detector:

```bash
go test -race ./...
```

Run tests for one package:

```bash
go test ./internal/algorithm -v
go test ./internal/simulation -v
go test ./internal/risk -v
go test ./internal/broker -v
go test ./internal/session -v
```

## Current Test Coverage

The current tests verify:

- VWAP calculations.
- Standard deviation and VWAP bands.
- Buy, Sell, and Hold decisions.
- Zero-volume behavior.
- Annualized volatility.
- Invalid volatility input.
- Kelly fraction.
- Stop-loss validation.
- Cash and buying-power limits.
- Stock-allocation limits.
- Order creation.
- Next-bar fills.
- Prevention of cross-symbol fills.
- Buy and Sell account updates.
- Position reset after selling.
- Portfolio-allocation validation.

## Current Development Status

Completed:

- Core trading pipeline.
- Multi-stock portfolio state.
- Historical market-data fetching.
- Pending-order execution.
- Portfolio accounting.
- Snapshot generation.
- Unit-testing phase.

Current phase:

```text
Phase 3 complete: HTTP contract, reusable service, bounded background jobs,
and API integration/concurrency verification.
Next: Phase 4 — SSE historical replay.
```

## Roadmap

### Phase 1 — Reusable Backtest Engine

- Define backtest input and result structures.
- Move the backtest loop out of `main()`.
- Add multi-stock integration tests.

### Phase 2 — External Data Smoke Testing

- Run controlled Alpaca and FRED tests.
- Validate returned timestamps and market data.
- Verify final portfolio accounting.

### Phase 3 — HTTP API

- Accept portfolio input through HTTP.
- Validate symbols, weights, capital, and dates.
- Start and retrieve backtest results.

### Phase 4 — SSE Streaming

- Stream one snapshot at a time.
- Preserve chronological event ordering.
- Handle client disconnects and completion events.

### Phase 5 — Frontend Dashboard

- Portfolio-allocation form.
- Animated price chart.
- VWAP and band lines.
- Buy and Sell markers.
- Portfolio-equity chart.
- Position and order tables.

### Phase 6 — Backtest Reporting

- Total return.
- Profit and loss.
- Maximum drawdown.
- Win rate.
- Trade count.
- Sharpe ratio.
- Per-symbol performance.

### Phase 7 — Future Expansion

- Five-minute historical bars.
- Further market-data expansion.
- Split and dividend adjustments.
- Trading fees and slippage.
- Saved backtest results.
- Optional real-time market-data support.

## Known Limitations

- The application supports a terminal runner and a local HTTP API.
- Historical bars are currently daily bars.
- Symbols must return matching trading dates.
- Alpaca SIP daily bars are paginated; empty, malformed, and inconsistent series are rejected.
- Market data currently uses raw price adjustment.
- The current risk-free rate is applied across the backtest.
- Seed 0 uses time-based randomness; nonzero seeds support repeatable simulations.
- Trading fees and slippage are not yet included.
- Backtest results are not yet persisted.
- The frontend and SSE stream are not yet implemented.

## Disclaimer

ApexQuant is an educational backtesting project. It does not provide financial advice and should not be used as the sole basis for real investment decisions.

## Phase 2 verification

The provider client uses consolidated US SIP data, ascending daily bars, and all
pagination tokens. `CompletedDailyRange` excludes the current New York calendar
session to avoid partial daily bars. The default adjustment remains `raw`.
`Client` accepts an HTTP client and provider URLs for isolated tests; the engine
still receives `BacktestConfig` and performs no network or credential access.
The existing package-level fetch functions remain compatible wrappers.

Shared daily-bar validation rejects non-finite/non-positive OHLC, inconsistent
OHLC ranges, negative volume, invalid VWAP, zero timestamps, and duplicate or
unordered UTC dates. Zero-volume bars contribute no VWAP weight. Allocation
symbols normalize before duplicate checks; bars remain keyed by canonical symbol.
Initial accounts represent cash-only starts (equity equals cash); non-finite
configuration values are rejected. Matching multi-symbol dates remain mandatory.

Verified real-data run: September 5, 2025 through September 4, 2026, AAPL/MSFT at
50% each, $50,000 initial cash, 100,000 Monte Carlo paths, 252 steps, seed 42.
Each symbol returned 252 SIP daily bars. FRED returned 3.89% (September 3, 2026).
The run produced 504 snapshots, two buy fills, two sell fills, and 390 Hold
snapshots. Both positions ended with zero quantity; final cash, buying power,
and equity were $49,702.00601245. Every timestamp's accounting was reconstructed
independently from fills and closing prices. JSON encoding passed.

Ordinary tests use fake HTTP transports and require no secrets or network.
To repeat the optional provider/portfolio test, first export `APCA_API_KEY_ID`,
`APCA_API_SECRET_KEY`, and `FRED_API_KEY` securely in your execution environment:

```sh
APEXQUANT_LIVE_TEST=1 go test ./internal/backtest -run '^TestLivePortfolioReconciliation$' -count=1 -v -timeout 10m
```

The live test deliberately uses the fixed verification dates above. Data providers
may revise history; a fixed seed controls simulation randomness, not revisions.
The application does not automatically load `.env`; descriptive colon-separated
key notes are not shell environment assignments. Never commit credentials.

This verification establishes input and accounting correctness for the tested
run, not strategy profitability. Fees, slippage, corporate-action treatment,
historical risk-free-rate curves, and Monte Carlo concurrency optimization remain
future work. HTTP/SSE/UI are not implemented by Phase 2.

## Phase 3 progress

Task 1 defines the [HTTP API contract](docs/http-api.md): dedicated JSON models,
request validation, percentage/date conversion, and result mapping with explicit
unavailable values. Contract tests use synthetic inputs and the existing engine.
Task 2 adds a reusable backtest service, bounded background jobs, and HTTP handlers.
Task 3 passed the phase-wide API verification and review gate.


### Running the HTTP API

Export the three provider credentials as described above, then run:

```sh
go run ./cmd/server
```

The default address is `127.0.0.1:8080`. This is a local, single-user API with no
user authentication; it is not configured as a public deployment. Run
`go run ./cmd/server -h` to see configurable limits and simulation settings.
The terminal workflow is preserved at `go run ./cmd/backtest`.

Example submission (no provider keys belong in the request):

```sh
curl -i http://127.0.0.1:8080/api/backtests \
  -H 'Content-Type: application/json' \
  -d '{"initial_capital":50000,"allocations":[{"symbol":"AAPL","percent":50},{"symbol":"MSFT","percent":50}],"start_date":"2025-09-05","end_date":"2026-09-04"}'
```

Use the returned ID to query `/api/backtests/{id}` and, after completion,
`/api/backtests/{id}/result`. A request disconnect does not cancel an admitted job.
Jobs and results are lost on process restart. The service uses one FRED rate as of
the requested end date across the simulation; it does not yet model a historical
rate curve or vintage publication availability.

Default protection settings:

| Setting | Default |
| --- | --- |
| Running jobs / waiting queue | 1 / 4 |
| Request body / headers | 16 KiB / 16 KiB |
| Simultaneous HTTP handlers | 16 |
| Requested range | 366 calendar days |
| Estimated snapshots | 2,928 (calendar days times symbols) |
| Shared request bucket | 120/minute refill, burst 30 |
| Shared submission bucket | 6/minute refill, burst 2 |
| Finished records / retention | 20 / 30 minutes |
| Serialized result / all stored results | 16 MiB / 64 MiB |

Rate limits are shared across local clients, including failed requests. Status
polling once per second fits the normal request budget. Exhaustion returns 429;
a full queue or simultaneous-handler limit returns 503. Oversized input returns
413, unsupported content type returns 415, and an excessive requested workload
returns 400. No limit silently lowers simulation fidelity or skips bars.
Finished jobs are evicted oldest first when retention/count/byte limits are hit;
expired or evicted IDs return 404. A result that exceeds its individual limit
fails explicitly rather than returning truncated snapshots. Byte limits bound
retained serialized results; temporary computation/encoding allocations are
additionally constrained by the input range and worker count.

HTTP timeouts are 5s for headers, 10s for request reading, 30s for response writing,
and 60s for idle connections. Provider requests retain their 15s timeout.
Shutdown stops admission, cancels queued/provider work, and allows up to 30s for
HTTP requests and running jobs to finish. The engine has no mid-computation
cancellation: expiration of that grace period ends the process, not a successful
job. Monte Carlo's internal goroutine fan-out is unchanged.

Task 2 tests use fake providers with real handlers/service/engine and verify job
admission, transitions, retrieval, rate/capacity limits, retention, safe failures,
and shutdown. No real provider calls are required by these tests.


### Phase 3 final verification (2026-09-30)

`internal/api/phase3_test.go` adds the final integration checks:

- A synthetic trading portfolio passes through handlers, jobs, service, and engine.
  Its complete API result matches a separately executed engine result after
  excluding randomly generated order IDs. The fixture verifies next-open buy
  and sell fills, final accounting, and no final-bar submission.
- Both bar-fetch and FRED failures become safe failed-job responses. Mismatched
  symbol dates fail explicitly. Failed results return 409; expired IDs return 404.
- Concurrent HTTP submissions stay within queue capacity and preserve unique job
  IDs. Status polling, shutdown admission, and in-flight request recovery work.
- A real localhost HTTP client/server round trip retrieves a completed result.
  External market data is fake; no provider credentials are required.
- A regression test reproduced extra token credit from out-of-order request
  timestamps. The limiter now advances its refill clock only for later timestamps.

The full uncached test suite, race suite, vet, both executable builds, and
`git diff --check` passed. The API package reached 92.3% statement coverage.
The concurrency/limiter/in-flight tests also passed 10 repeated race-enabled runs.
The localhost test requires permission to bind a local port in sandboxed runners.

This verifies Phase 3's local API behavior. No new authenticated provider run or
public deployment was performed. `cmd/server` startup/signal wiring was built but
is not covered by automated startup tests; integration coverage exercises the
HTTP handlers over a local listener. Existing limitations remain: in-memory
storage, no authentication, no mid-engine cancellation, no SSE or frontend yet.
No commit or push is implied by passing the phase gate.
