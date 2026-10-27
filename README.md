# ApexQuant

ApexQuant is a Go-based multi-stock portfolio backtesting application.

Users select between 1 and 8 stock symbols, assign each stock a percentage of the portfolio, and run historical backtests using daily market data. The dashboard supports date presets and custom ranges; the terminal defaults to the last seven completed calendar days. Minute execution is temporarily disabled pending long-history storage and performance work.

The application calculates indicators, generates trading signals, sizes positions using Monte Carlo simulation and Kelly-based risk management, and tracks orders, positions, cash, buying power, and portfolio equity.

> ApexQuant is currently under active development and is intended for educational and research purposes.

## Current Features

- User-selected portfolio of 1–8 stocks.
- Portfolio allocations totaling 100%.
- Daily historical bars from Alpaca; minute execution code is retained but disabled.
- Historical risk-free-rate vintages from FRED, selected by decision date.
- Independent state for every stock.
- Rolling volume-weighted average price (VWAP), with a configurable daily window.
- VWAP standard-deviation bands over the same window.
- Buy, Sell, and Hold signals.
- Annualized historical volatility.
- Monte Carlo simulation with plain Geometric Brownian Motion or Auto/Manual
  VWAP mean reversion using log-price Ornstein-Uhlenbeck dynamics.
- Fractional Kelly sizing using target, stop and timeout payoffs.
- Cash, buying-power, and allocation limits.
- Entry orders submitted at the current close and filled at the next available open.
- Protective OHLC exits and a shared simulation/execution holding deadline.
- Portfolio-wide equity calculation.
- Submitted and filled-order snapshots with decision and rejection diagnostics.
- Interactive dashboard and synchronized historical SSE replay.
- Unit tests for algorithms, risk, simulation, broker, and session logic.

## Backtest Flow

For each historical trading day, ApexQuant performs the following sequence:

```text
1. Update every stock to its opening price
2. Calculate opening portfolio equity
3. Settle pending entries from the previous bar
4. Process protective and holding-deadline exits
5. Update every stock to its closing price and calculate closing portfolio equity
6. Update rolling VWAP/bands, volatility and the available historical rate
7. Detect eligible Buy opportunities or record Hold/Sell reasons
8. Simulate eligible opportunities using the selected model
9. Apply timeout-aware Kelly sizing and portfolio risk limits
10. Submit next-open entry orders
11. Record snapshots for results and replay
```

The final historical bar may fill an existing pending order, but it cannot submit a new order because no future bar exists to fill it.

## Trading Strategy

### Buy Signal

A Buy signal is generated when:

```text
No position is currently open
Standard deviation is greater than zero
Today's low is at/below the previous completed lower band,
  OR today's close is at/below the current completed lower band
No exit has already occurred in this bar
```

The initial risk levels are:

```text
Entry       = signal close (actual entry fills at the next available open)
Stop loss   = signal close - standard deviation
Take profit = VWAP + 0.5 × standard deviation
```

### Sell Signal

A protective exit checks opening gaps first, then the daily low/high:

```text
Opening price crosses stop/target -> exit at the opening price
Daily low <= stop-loss price      -> exit at the stop
Daily high >= take-profit price   -> exit at the target
```

If both boundaries touch, the stop takes priority. The configured holding period
defaults to five trading bars including entry day; absent a protective exit, the
position exits at the deadline close. Zero volume can delay execution. No same-bar
reentry occurs. An entry opportunity is not a guaranteed submission or fill.

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
ALPACA_API_KEY
ALPACA_API_SECRET
FRED_API_KEY
```

Example:

```bash
export ALPACA_API_KEY="your-alpaca-key"
export ALPACA_API_SECRET="your-alpaca-secret"
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
Phase 4 complete: replay timeline, bounded SSE transport, and phase-wide verification.
Phase 5 complete: local dashboard, portfolio/settings form, input validation,
backtest status tracking, animated charts, replay controls, daily account/position
views, order history, historical activity, run configuration and dashboard verification.
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

Tasks 1–2 add the [replay contract and SSE transport](docs/replay.md), with
deterministic event ordering, daily portfolio updates, playback speed, resumable
event IDs, disconnect handling and separate stream limits.

### Phase 5 — Frontend Dashboard

- Portfolio-allocation form.
- Animated price chart.
- VWAP and band lines.
- Buy and Sell markers.
- Portfolio-equity chart.
- Position and order tables.

The dashboard is in `web/assets` and embedded in the Go server executable.
Open `http://127.0.0.1:8080/` after starting `go run ./cmd/server` with the existing
provider environment variables. End users do not need a Node.js server or a
separate frontend build. The interface uses a dark theme with teal, gold and coral neon
accents and adapts to narrow screens.

The Run Builder is inside Backtest Settings. The right-side portfolio panel shows
the displayed day's equity, cash, buying power and open positions, with four stock
cards per page. Selecting a card switches the price chart. The equity header shows
value and return relative to starting capital; hover over the equity chart or use
its arrow keys to inspect each displayed day. Chart lines use a soft neon glow. At 5, 10 and 20 days/sec, the chart camera
pans steadily across the full day interval rather than stopping between bars.

The portfolio form and settings dialog validate allocations, capital, completed dates,
and current server limits from `GET /api/config`. Job tracking distinguishes
queued/running/completed/failed states, retrieves a completed result, and can
resume interrupted polling without submitting another backtest. It does not
invent computation progress or automatically retry uncertain submissions.
Settings offer starting capital, date presets and custom dates; automatic
volatility is described, but manual volatility overrides remain unimplemented.
The completed run automatically starts a historical SSE replay. Price candles,
VWAP/bands, filled Buy/Sell markers and portfolio equity advance together only
after every stock's snapshot for a day has arrived. Hold days are retained.
Pause/resume, previous/next day, restart, received-history seeking and playback
speed controls are available. The price chart shows a recent window suited to
the screen; equity retains all displayed days. Replay does not rerun the engine.
Smooth chart motion eases scrolling and axis rescaling at the selected playback
speed, without interpolating market prices or account values. Transitions last up
to 600 ms and shorten at faster playback speeds. Pause, stepping, seeking, stock
changes and resizing settle immediately. The checkbox can disable motion, and
the operating system's reduced-motion preference is always respected.
Account cards and position tables follow the displayed replay day. Positions show
shares, entry/closing prices, market value, unrealized P&L, requested versus actual
allocation, and stop/take-profit levels. Flat positions retain zero holdings value
and unavailable entry/exit levels. Return since start uses equity versus starting
capital; unrealized P&L describes open positions only.

Orders are deduplicated by ID and show their last observed status through that
day, submission date, fill date and execution price. Cancellations/rejections are
not retained by the engine and cannot be inferred from a missing fill. Historical
activity includes every bar, including Hold, plus recorded fills and submissions.
Both histories paginate at 20 rows and rewind with the charts; future activity
is not shown early. The configuration disclosure shows the completed run's
request, model settings and data metadata independently of edits for a new run.
It contains no credentials and does not save the run to disk. Chart areas contain
no illustrative market or profit data before a run.

Development checks for these modules use Node.js (no third-party packages):

```sh
cd web
npm test
```

Phase 5 browser checks exercise allocation/capital validation, applied settings,
keyboard dismissal, narrow-screen layout, submission/completion/failure, rewind,
next-open fills, position/account agreement, configuration isolation, pagination,
multiple runs and smooth camera motion, including fast playback at 5, 10 and
20 days/sec. Synthetic inputs
pass through the real engine/job/result/replay handlers. Module tests also cover
interrupted tracking, duplicate submission prevention, request timeouts, backend
limit agreement, motion cancellation and exact camera settlement. No live
provider run was required for these UI checks.

#### Historical volatility preparation

Before trading starts, the shared service fetches earlier daily prices to prepare
each stock's volatility estimate. With the default 20-bar window, the request
extends 54 calendar days before the selected start to allow for weekends and
holidays, and retains the latest 20 earlier bars per stock in Off/Manual mode.
Auto requests additional pre-run history for its regression: up to 252 transitions
plus the selected VWAP window. This is a bounded history request, not a guarantee
that every symbol has enough available bars.

At each trading day's close, volatility uses the latest available bars through
that close, up to the configured window. It is the sample standard deviation of
daily log returns multiplied by the square root of 252 (by default). Earlier
preparation bars roll out as trading bars arrive. Future prices are never used.

Preparation bars do not create trades, change starting capital, initialize the
trading-run VWAP/bands, or appear in result snapshots or replay events. Auto uses
them separately to calculate historical calibration VWAPs. If fewer than three
total bars are available, the configured fallback volatility (20% by default)
still applies until an estimate can be calculated. `initial_volatility` in API
settings continues to describe that fallback, not each stock's rolling estimate.
Simulation count remains a backend setting; dashboard controls for simulation
count and manual volatility overrides have not yet been implemented. Regression tests check the
first-day estimate against an independent calculation, rolling-window behavior,
isolation between symbols, exclusion of future prices, and separation of
preparation data from trading and replay data.

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
- Fully or Partial sell Order. 
- Trading fees and slippage.
- Saved backtest results.
- Expand date presets and verify longer runs against live provider history.
  January 1, 2017 is the earliest accepted start date, subject to each
  symbol's actual data availability. Custom dates support up to 3,660 calendar
  days by default; longer histories require more calculation time and memory.
- Optional real-time market-data support.
- Move the main GBM/Monte Carlo calculation core from Go to C++ in a later
  implementation phase to optimize calculation speed. Keep Go orchestration and
  verify numerical correctness, reproducibility, and measured performance against
  the Go implementation before replacing the core.

## Intraday execution and chart intervals

The dashboard defaults to **daily execution**, $50,000 and AAPL/NVDA allocations of 50% each. The terminal uses the same service:

```bash
go run ./cmd/backtest -timeframe 1Min -days 7
# Legacy daily comparison:
go run ./cmd/backtest -timeframe 1Day -days 365
```

API requests currently accept `"timeframe": "1Day"` or omission. `"1Min"` requests are rejected with `minute_execution_disabled` before queueing or fetching data. Minute implementation and tests are retained for future use. The calendar limit remains 3,660 days, with a default budget of 29,280 estimated snapshots. Minute requests count 390 bars per calendar date per symbol, so their default range is shorter (for example, 25 calendar dates for three stocks). Daily execution supports the full ten-year range for eight stocks. Explicit server overrides can increase these limits, but are not a guarantee that a run will fit memory. Results and replay history remain in memory: defaults allow 64 MiB per result, 256 MiB retained results and 128 MiB for each replay source/timeline budget. Replay connection and result-retention limits remain three hours.

Minute execution is temporarily disabled in the dashboard, CLI and API/service. The retained minute engine uses the following explicit rules (for development tests, not currently available runs):

- VWAP and its volume-weighted bands reset each New York calendar session. They accumulate the provider's minute VWAPs; the band variance is between minute VWAPs, not reconstructed tick variance. EMA20 uses minute closes, continues between sessions, and is marked as warming for the first 19 bars. EMA slope describes momentum; it never vetoes a dip buy.
- When flat, a close at/below the current lower band or a low touching/crossing the **previous completed minute's** lower band produces a Buy opportunity. Current-bar lows are not compared with a boundary that was unknown during that bar. A recovered close that leaves no positive target distance is explicitly rejected. There is no new breakout-buy rule.
- Signals are evaluated after a completed minute. Eligible buys fill at the following consecutive minute's open, subject to cash, buying power and allocation caps. One pending entry per symbol; no pyramiding. Stops and targets retain their signal-time distances relative to the actual entry fill. Pending entries expire on a missing/zero-volume next minute or a new session.
- Held positions have protective exits active independently of Kelly. Exact stop/target touches count. Opening gaps execute at the observed open. When both levels occur inside a minute with the open between them, stop-first is the conservative assumption. An entry at the open can be stopped/targeted within that same minute. Both fills are retained; there is no same-minute reentry after an exit.
- Final bars cannot submit a new order. The 15:59 bar cannot submit an overnight entry. Open positions may carry overnight; they are marked at the end rather than forcibly liquidated. Zero-volume bars cannot execute orders. Missing or mismatched minute timestamps across stocks reject the run explicitly instead of inventing bars.
- GBM volatility uses up to 20 **completed earlier daily bars**, annualized with 252 periods/year. It does not substitute 20 minutes for 20 days or use the current session's future daily close. The five-day horizon still means five trading days, not five minutes. Kelly retains its 0.2 multiplier and all funding/allocation caps.

In the dashboard, **1 minute / 5 minutes / 1 day** changes candle display only. It aggregates the received replay prefix, preserves all fill markers, and samples the same minute VWAP/EMA at each displayed candle's end. Switching chart intervals never changes orders. Legacy daily data cannot be expanded into genuine minute candles.

Minute snapshots add `filled_orders`, `order_events`, `decision_reason`, `execution_reason` and, when sizing is evaluated, `risk` probabilities/Kelly/quantity. `filled_order` remains a compatibility alias for the last fill. The event table and terminal explain sizing rejections instead of silently presenting only Hold. Signals and submitted orders remain distinct from fills.

These are historical simulation assumptions, not broker execution guarantees. The trading window is fixed at 09:30–16:00 New York; an exchange early-close calendar is not yet applied. Minute OHLC cannot reveal tick order or the exact instant of an intraminute fill. Next-open entry assumes zero processing latency; fees, spread, queue position and slippage remain unmodeled. [Alpaca labels minute bars by their start time](https://alpaca.markets/learn/stock-minute-bars); signal timestamps therefore advance by one minute. No profitability improvement is claimed from these correctness changes.

## Known Limitations

- The application supports a terminal runner and a local HTTP API.
- Historical execution supports one-minute bars and legacy daily bars.
- Symbols must return matching timestamps for minute runs, or matching dates for daily runs.
- Alpaca SIP bars are paginated; empty, malformed, and inconsistent series are rejected.
- Market data currently uses raw price adjustment.
- The current risk-free rate is applied across the backtest.
- Seed 0 uses time-based randomness; nonzero seeds support repeatable simulations.
- Trading fees and slippage are not yet included.
- Backtest results are not yet persisted.
- The SSE stream replays completed results; it does not show live calculation progress.
- Saved-run history, names/notes and durable storage are not yet implemented.
- Replay advances only after all stocks at the current execution bar are received. Display aggregation does not create finer data.

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

Historical Phase 2 verification (before the later daily-strategy/model changes;
these are not current-model benchmark results): September 5, 2025 through
September 4, 2026, AAPL/MSFT at
50% each, $50,000 initial cash, 100,000 Monte Carlo paths, 252 steps, seed 42.
Each symbol returned 252 SIP daily bars. FRED returned 3.89% (September 3, 2026).
The run produced 504 snapshots, two buy fills, two sell fills, and 390 Hold
snapshots. Both positions ended with zero quantity; final cash, buying power,
and equity were $49,702.00601245. Every timestamp's accounting was reconstructed
independently from fills and closing prices. JSON encoding passed.

Ordinary tests use fake HTTP transports and require no secrets or network.
To repeat the optional provider/portfolio test, first export `ALPACA_API_KEY`,
`ALPACA_API_SECRET`, and `FRED_API_KEY` securely in your execution environment:

```sh
APEXQUANT_LIVE_TEST=1 go test ./internal/backtest -run '^TestLivePortfolioReconciliation$' -count=1 -v -timeout 10m
```

The live test deliberately uses the fixed verification dates above. Data providers
may revise history; a fixed seed controls simulation randomness, not revisions.
The application does not automatically load `.env`; descriptive colon-separated
key notes are not shell environment assignments. Never commit credentials.

This verification establishes input and accounting correctness for that tested
version, not strategy profitability. Fees, slippage and corporate-action treatment
remain future work. Later phases added HTTP/SSE/UI, historical rate vintages,
and Go simulation optimizations; see the current model sections below.

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
Jobs and results are lost on process restart. The production service batches FRED
historical vintages and selects the available rate at each decision date. Legacy
providers exposing only a single-rate method use a fixed rate from before the run.

Default protection settings:

| Setting | Default |
| --- | --- |
| Running jobs / waiting queue | 1 / 4 |
| Request body / headers | 16 KiB / 16 KiB |
| Simultaneous HTTP handlers | 16 |
| Requested range | 3,660 calendar days (roughly ten years) |
| Estimated snapshots | 29,280 (minute requests count 390 bars per date per symbol) |
| Shared request bucket | 120/minute refill, burst 30 |
| Shared submission bucket | 6/minute refill, burst 2 |
| Finished records / retention | 20 / 3 hours after completion |
| Serialized result / all stored results | 64 MiB / 256 MiB |

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
HTTP handlers over a local listener. Limitations at Phase 3 completion included: in-memory
storage, no authentication, no mid-engine cancellation, no SSE or frontend yet.
No commit or push is implied by passing the phase gate.

### Phase 3 calendar-date correction

The shared service interprets the run's end instant in `America/New_York` before
querying FRED. This prevents an inclusive market end-of-day timestamp from
selecting a following-day observation after conversion to UTC. Alpaca's UTC
timestamp bounds remain unchanged. Direct FRED callers use the calendar date in
the supplied time's location; existing UTC date-only callers keep their date.
Regression tests cover API and terminal range construction in summer and winter,
the outbound FRED date, and rejection of following-day observations.

### Phase 4 replay transport

After a job completes, stream its cached result with:

```sh
curl -N 'http://127.0.0.1:8080/api/backtests/{id}/replay?speed=1'
```

Replace `{id}` with the returned job ID. Speed is historical days per second
(0.25–20, default 1). A replay includes Start, every stock snapshot including Hold,
one portfolio update per day, then Complete. Reconnect with `Last-Event-ID` to
continue after the last received event while the job is still retained.

Defaults allow two replay connections with 128 MiB each for serialized source and
timeline budgets, a five-second deadline per write/flush, and a three-hour lifetime
per connection. Result retention is three hours after job completion, subject to
earlier count/byte eviction; starting a replay does not renew that retention timer.
These streams use separate capacity from ordinary requests. They do not rerun the
engine, pin expired jobs, or silently skip bars. See the
[replay contract](docs/replay.md) for error, completion and timeout semantics.

Task 2 verification uses fake providers and real localhost HTTP/TCP tests for
ordered delivery, resume, disconnect cleanup, slow-reader deadlines and playback
across a short server write timeout.

Task 3 verification passed on 2026-10-06. End-to-end tests cover HTTP/1.1 and
HTTP/2 delivery, independently checked event order, filled-order accounting,
changing portfolio values on Hold days, unavailable versus zero indicator values,
concurrent stream admission and capacity recovery, and fresh connection deadlines.
The three-hour lifetime and retention checks use controlled timestamps rather
than waiting three hours. The full race suite passed with 93.4% API statement
coverage; static analysis and both server and CLI builds also passed.

Market View offers 1-minute, 5-minute and daily display candles for minute executions, independently of the trading interval. Use − / + to show more/fewer candles or All to fit the received history. Daily execution offers daily candles only; it cannot reconstruct minute prices. Your portfolio includes a recent equity mini-chart alongside the stock-card charts.

Daily execution now uses **rolling VWAP and bands**, defaulting to the latest 20 observed trading days within the requested run. The same window supplies the volume-weighted mean and dispersion of daily bar VWAPs. Early bars use the available prefix; zero-volume bars occupy a day in the window but add no weight. This is not reconstructed tick-level variance. Older observations drop out as the window advances, allowing bands to follow a sustained change in price range.

Set **Daily VWAP window** (2–252 trading days) in Backtest Settings when using daily execution, send `"vwap_window":60` in a daily API request, or use `go run ./cmd/backtest -timeframe 1Day -days 365 -vwap-window 60`. Omission uses 20. Minute execution retains session VWAP and does not accept a daily window. The returned `settings.daily_vwap_window` records the effective daily setting. Existing saved results without this field retain their legacy indicator interpretation.

Entry touches use the previous completed day's band; the current completed daily window supplies close-based entry eligibility and signal-time stop/target distances. Entries still fill next open. Protective exits, no same-bar reentry, and cash/allocation/Kelly gates are unchanged. No breakout-buy rule or profitability improvement is implied. Daily bands are calculated only from bars up to the displayed day.

Minute execution is paused until disk-backed histories, bounded processing/replay and performance measurements can support long runs. A local SQLite database is a proposed storage option; it is not implemented. Increasing memory ceilings alone is insufficient. Daily execution, including rolling VWAP, remains available.


### Model/execution consistency

Daily runs now share a maximum holding period with simulation: `holding_days`
(default 5; 1–252 trading bars, including the actual entry day). Stop/target
exits take priority; otherwise the deadline exits at that day's close. With no
volume, an overdue exit waits for an executable bar and uses its open (opening protective gaps keep their reason; later
wicks cannot precede that open exit). No reentry occurs on an exit bar. If the data
ends before the deadline, the remaining position is marked at the final close;
that truncated trade has not completed the modeled horizon.

The dashboard Settings and CLI `-holding-days` expose the period. Simulation
uses 252 trading days/year, dividing its step budget into equal daily groups
(the effective step count rounds up to a multiple of holding days). Each group
forms a synthetic OHLC bar using sampled prices, with the same stop-first
convention as daily execution. This finite grid can miss between-sample touches;
simulated paths do not model overnight gaps or transaction costs. Entry forecasts
are conditional on the signal price; actual next-open gaps remain a modeling
limitation even though execution reanchors stop/target distances at the fill.

Every simulation path retains a payoff in units of planned stop risk, including
terminal profit/loss when neither barrier hits. Kelly maximizes empirical mean
log wealth over all these outcomes, then the existing 0.2 sizing multiplier and
cash/allocation caps apply. Barrier-win probability is not total probability of
positive return: profitable timeouts remain classified as timeouts. Raw samples
are transient and are not sent to the dashboard. Diagnostics retain the timeout
mean return and actual decision rate/model.

The default probability model remains `gbm` (risk-free drift, no VWAP recovery
assumption). Optional `vwap_reversion` models log price with an OU pull toward
VWAP fixed at the signal. Its manual `reversion_half_life_days` defaults to 5
and must be positive and at most 252. The pull and risk-free log drift are
separate terms; a half-life describes decay of distance without drift/noise.
This is an explicit model assumption, not calibrated evidence of recovery or a
profitability guarantee. CLI: `-probability-model vwap_reversion -reversion-half-life 5`. Auto is available as `vwap_reversion_auto`; Manual remains `vwap_reversion`, and Off is `gbm`.

Production rate inputs use batched DGS3MO historical vintages, selected locally
for each decision. A vintage becomes eligible the calendar day after both its
observation and vintage dates, conservatively avoiding unknown intraday release
timing. Older revisions cannot displace newer available observations. Missing
initial history fails the run; holidays carry the latest available value. Legacy
custom providers without the history method use an explicit fixed-before-start
policy. Result metadata identifies the policy and the initial/fixed rate;
per-decision diagnostics report the rate actually used.

One provider client caches up to eight exact date ranges in memory, coalesces
identical concurrent history requests, and spaces FRED history attempts by at
least 600 ms (100/minute). 429 responses use bounded cancellable backoff. Other
processes sharing the key are outside this limiter. Pagination/vintages may
require multiple requests; there are no requests per bar, symbol or simulation
path. The cache is not a saved-run database and clears on restart.

FRED history requests bound both the observation range and the vintage range.
Vintage windows span at most four calendar years (at most 1,461 distinct dates),
below FRED's 2,000-vintage-date JSON limit; observation pagination resets for each
window. Longer backtests merge those windows before date-based selection. Provider
failures identify stock-price (Alpaca) versus interest-rate (FRED) loading without
exposing provider response bodies, request URLs or credentials.


### Automatic VWAP mean reversion

The dashboard exposes Off (plain GBM), Auto, and Manual (half-life override).
Auto fits `log(P[t+1]/P[t]) = c + b*log(VWAP[t]/P[t]) + error`, using
only preparation bars strictly before trading begins. VWAP uses the selected
rolling window and a complete window at each calibration observation. Measuring
actual price returns excludes mechanical VWAP catch-up from the response.
An intercept separates background drift from the distance response.
Up to 252 transitions are used, with at least 60 required; the 95% approximate
Newey-West (5-lag) slope interval must lie strictly inside (0,1). Half-life is
`ln(2)/[-ln(1-b)]`, bounded to 0.1–252 trading days. Fitted annual log drift is
`c * [-ln(1-b)] / b * periods_per_year`, matching the one-day conditional mean
of the OU simulation. Rolling market volatility is still estimated separately.
With drift, the long-run expected log level can be offset from VWAP; half-life
measures decay around that level. This is empirical calibration, not proof of
causation, a validated forecast, or a guarantee of trading profits.

Estimates are frozen for the entire run (they may become stale in long runs),
recorded per symbol in `settings.reversion_estimates`, and never silently replaced
with manual values or GBM. Any unreliable symbol fails the job with
`unreliable_reversion`; the dashboard displays a dismissible corner notification with a Review settings button; inputs remain preserved.
Changing the VWAP window recalibrates Auto on the next run. Auto requires daily
execution and may need history before the earliest permitted trading start.
