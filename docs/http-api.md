# Backtest API contract

Task 1 defines the contract; Task 2 implements HTTP routes and bounded job
execution. The engine remains independent of the API package.

## Dashboard input limits

`GET /api/config` returns public form limits: `earliest_start_date`,
`latest_end_date`, `max_range_days`, `max_snapshots`, `max_symbols`,
`percent_total_tolerance`, and `market_timezone`. The latest end date is the
previous completed New York calendar day, allowing for the provider's 15-minute
delay. Limits reflect this server's configuration, rather than browser defaults.
Responses are not cached and contain no provider credentials. This read-only
endpoint shares ordinary request rate and concurrency limits.

## Create

Start dates must be on or after January 1, 2017, subject to the selected symbols'
available market history. The default maximum range is 3,660 calendar days
(roughly ten years), with a 29,280 calendar-days-times-symbols workload budget.
The dashboard reads both limits from the server; smaller flag overrides still apply.

`POST /api/backtests`, with `Content-Type: application/json`:

```json
{
  "initial_capital": 50000,
  "allocations": [
    {"symbol": "AAPL", "percent": 50},
    {"symbol": "MSFT", "percent": 50}
  ],
  "start_date": "2025-09-05",
  "end_date": "2026-09-04"
}
```

The four fields in this example are required. Capital must be finite and positive.
Select 1–8 symbols. Optional strategy fields are:

| Field | Behavior |
| --- | --- |
| `timeframe` | Omitted or `1Day`; `1Min` is temporarily disabled at admission |
| `vwap_window` | Daily rolling VWAP/band window, 2–252 bars; default 20 |
| `holding_days` | Shared execution/simulation deadline, 1–252 bars; default 5 |
| `probability_model` | `gbm` (Off/default), `vwap_reversion_auto` (Auto), or `vwap_reversion` (Manual) |
| `reversion_half_life_days` | Manual only, positive and at most 252; omitted/zero resolves to 5 in the service |

Auto uses only pre-run history and records per-symbol estimates in resolved
settings. An unreliable fit fails the job with `unreliable_reversion`; it never
silently falls back to another model. See [replay/model details](replay.md) for
the estimator, reliability screen and frozen-fit limitations.

Symbols are trimmed, uppercased, checked against the supported ticker syntax,
and checked for duplicates. Each percentage is positive and at most 100. Their
sum must be within 0.0001 percentage points of 100, matching the engine's
0.000001 fractional tolerance. Percentages convert to weights by dividing by 100;
they are not silently rebalanced. Input slices are not mutated.

Dates must use YYYY-MM-DD, with start strictly before end. Both calendar dates
are inclusive, interpreted in America/New_York. Fetch boundaries are converted
to UTC, respecting daylight saving time. The end becomes 23:59:59 New York time
and must be at least 15 minutes before the supplied current time. This
conservatively excludes the current day even if its regular session has closed.
Weekend/holiday dates are permitted; returned bars still require the engine's
minimum count and matching timelines. The API does not silently intersect dates.

Credentials, path/step counts, random seed and volatility settings are server-owned;
the strategy fields above are user configurable. Unknown JSON
fields, invalid types, null root values, and trailing JSON are rejected.
Missing or null individual required fields fail semantic validation.
Handlers enforce application/json and configured request body limits.

Successful admission returns `202 Accepted`:

```json
{"id":"example-backtest-id","status":"queued"}
```

This confirms admission, not successful market-data fetching or computation.

## Status

`GET /api/backtests/{id}` returns `200 OK`:

```json
{"id":"example-backtest-id","status":"running","error":null}
```

Job states: queued -> running -> completed or failed. No percentage is promised.
A failed job returns the same shape with status `failed` and a populated safe
error. Server restart clears the in-memory jobs. Finished jobs expire after
three hours after completion by default, or earlier when record/byte budgets
require eviction. This retention timer is independent of each replay connection's
lifetime; starting or reconnecting a replay does not renew stored-job retention.

## Result

`GET /api/backtests/{id}/result` returns `200 OK` only for completed jobs.
`BacktestResultResponse` contains:

- `id`, normalized `request`, server `settings`, and `data` metadata.
- `final_account`: `cash`, `equity`, `buying_power`.
- `final_positions`: positions keyed by canonical symbol.
- `snapshots`: the full engine sequence, chronological with allocation order
  within each aligned UTC calendar date. Each symbol snapshot retains the shared account values.

Snapshot fields: `timestamp`, `bar`, `indicator`, `signal`, `position`, `account`,
`submitted_order`, `filled_order`, plus optional `filled_orders`, `order_events`,
`decision_reason`, `execution_reason`, and `risk`. `filled_orders` preserves multiple
fills in a bar; `filled_order` remains the legacy last-fill view. Risk diagnostics
retain model, decision rate, target/stop/timeout probabilities, timeout mean return,
Kelly fraction and planned quantity. Timestamps serialize in UTC RFC3339 format.
Actions/status strings are lowercase. Buy signals are distinct from submitted
orders. Hold signals have a symbol and snapshot timestamp, with null signal price.
Absent orders serialize as null. Submitted orders have null `filled_price`.
Flat positions have null entry/stop/target levels; engine candidate levels on a
flat position must not be presented as an executed position.

The result includes configured Monte Carlo paths, steps, horizon, initial
volatility, seed, volatility window, annualization, daily VWAP window, holding
period and model. Auto adds per-symbol `reversion_estimates`, including half-life,
strength/uncertainty, fitted drift, sample count and last preparation date. Seed zero still means
randomized execution; it is not an exact captured seed. Metadata identifies feed,
timeframe, adjustment, rate policy, and initial/fixed rate. Production historical
vintages populate `risk_free_rate_date` with the initial observation date;
legacy fixed-rate providers may leave it null. Per-decision diagnostics carry
the rate actually used. Auto's fitted drift is separate from this risk-free rate.

Before any positive volume, daily mapped indicators are null. Daily VWAP and bands
use the selected rolling window, not a cumulative whole-run calculation. A single
zero-volume bar contributes no weight; its individual bar VWAP is null.
Known limitation: the daily mapper still tracks whether a symbol has ever had
positive volume. If all positive-volume bars leave the rolling window, it emits
cleared zero-valued bands rather than unavailable values. Window-aware availability
mapping and regression coverage remain follow-up work; the engine itself marks
that window unavailable and does not generate a buy from those cleared bands.
A valid standard deviation of zero remains numeric zero. Uncomputed Z-score and
unretained signal probabilities are omitted. Non-finite engine results are rejected
rather than silently converted to null.

The mapping owns its output data and does not modify the engine. Map the full
history before any later pagination/replay, because retained per-symbol availability
cannot be inferred from an isolated slice.

## Errors

For `GET /api/backtests/{id}/replay`, event payloads, pacing, resume semantics,
and stream limits are documented in the [historical replay contract](replay.md).
Only completed retained jobs can start an SSE replay.

```json
{
  "error": {
    "code": "invalid_allocations",
    "message": "Allocation percentages must total 100.",
    "field": "allocations"
  }
}
```

`field` is omitted for errors unrelated to one input field. Field paths can name
an allocation index, for example `allocations[1].symbol`. Messages are safe for
clients; credentials and raw provider bodies must not be included.

Handler statuses:

| Status | Meaning |
| --- | --- |
| 400 | Invalid JSON or request fields |
| 404 | Job not found |
| 409 | Result not available because job has not completed successfully |
| 413 | Request exceeds configured body limit |
| 415 | Unsupported content type |
| 429 | Shared request or submission rate limit exceeded |
| 503 | Job queue or simultaneous handler capacity unavailable |
| 500 | Unexpected server error or non-serializable result |

Input error codes: `invalid_json`, `invalid_capital`, `invalid_allocations`,
`invalid_symbol`, `duplicate_symbol`, `invalid_dates`. Result mapping uses
`invalid_result`. Environment failures use `internal_error`. Errors after
admission are recorded on the job, rather than changing the original 202 response.

## Task 1 checks

Tests cover JSON envelopes, strict decoding, normalization and copy ownership,
weight conversion/tolerance, invalid input, inclusive/DST date boundaries, the SIP
recent-data restriction, per-symbol indicator availability, valid zero values,
order/position nulls, non-finite results, and mapping an actual deterministic engine
result. No real provider credentials or network calls are required.


## Task 2 boundaries

`backtest.Service.Run` handles provider access and engine orchestration for both
CLI and HTTP callers. `api.ServiceRunner` maps its output using metadata supplied
by the server's provider configuration. Provider errors are sanitized and may
carry an allowlisted `provider` value (`alpaca` or `fred`). Production FRED requests
batch historical vintages using New York calendar dates, even when UTC end-of-day
falls on the following date. Each trading decision selects a previously available
observation without future backfill. Legacy single-rate providers use a fixed rate
from before the run. Alpaca keeps the original UTC timestamp bounds. Auto preflight
calibration occurs before the historical-rate request, so unreliable fits fail
without that request.

The job manager owns a fixed worker pool and bounded waiting channel. It retains
immutable encoded result bytes, returns copies, and evicts only finished jobs.
Create returns 202, a full queue returns 503, a not-yet-successful result returns
409, and an absent/expired ID returns 404. A failed job's status endpoint still
returns 200 with status `failed` and a safe error. Worker panics are isolated to a
failed job. No computation-progress percentage or cancellation endpoint exists.

See the README for all default limits and launch commands. Request and submission
rate limits are shared token buckets with bounded state. HTTP/body/header/workload
limits protect the server around the engine; they do not alter accepted job settings.
This protection does not replace future Monte Carlo performance/cancellation work.

Phase 3 Tasks 1–3 are implemented and locally verified. Final tests cover a
trading result against a direct engine run, provider failures, concurrent requests,
expiry, shutdown, overload recovery, and a localhost HTTP round trip. Phase 4
added SSE replay; Phase 5 added the dashboard. See the README for validation
evidence and limits.
