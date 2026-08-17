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
go run ./cmd/server
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

During the backtest, the terminal displays:

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
Extract the backtest loop into a reusable RunBacktest function
and add deterministic multi-stock integration tests.
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
- Alpaca pagination.
- Split and dividend adjustments.
- Trading fees and slippage.
- Saved backtest results.
- Optional real-time market-data support.

## Known Limitations

- The application currently runs through the terminal.
- Historical bars are currently daily bars.
- Symbols must return matching trading dates.
- Alpaca pagination is not implemented.
- Market data currently uses raw price adjustment.
- The current risk-free rate is applied across the backtest.
- Monte Carlo simulations currently use time-based random seeds.
- Trading fees and slippage are not yet included.
- Backtest results are not yet persisted.
- The frontend and SSE stream are not yet implemented.

## Disclaimer

ApexQuant is an educational backtesting project. It does not provide financial advice and should not be used as the sole basis for real investment decisions.