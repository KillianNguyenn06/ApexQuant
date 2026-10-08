# Phase 4 historical replay contract

Task 1 implements the replay timeline and JSON payloads in
`internal/api/replay.go`. Task 2 adds bounded SSE transport in `internal/api/sse.go`.
The input is one complete `BacktestResultResponse`, already mapped from a
completed engine result. Replay never runs the engine again.

## Event sequence

| Event | Payload | Frequency |
| --- | --- | --- |
| `start` | Job ID, request, simulation settings, provider metadata, total days and snapshots | Once, first |
| `snapshot` | Zero-based day index, zero-based snapshot index, complete existing snapshot DTO | Every stock at every historical daily index, including Hold |
| `portfolio` | Day index, UTC calendar date, shared closing account, all positions for that day | Once after all stock snapshots for a day |
| `complete` | Job ID, totals, final account and final positions | Once, last |

For two stocks and two days, the sequence is:

```text
1 start
2 day 0: first allocation's snapshot
3 day 0: second allocation's snapshot
4 day 0: portfolio
5 day 1: first allocation's snapshot
6 day 1: second allocation's snapshot
7 day 1: portfolio
8 complete
```

IDs are consecutive one-based integers, local to this job's completed timeline.
The same completed response yields the same IDs and ordering. IDs do not denote
durable saved runs. With D days and S stocks, there are D*S snapshot events,
D portfolio events, and `2 + D*(S+1)` total events. No empty timeline is emitted.

Dates advance strictly by the engine's **UTC calendar-date** alignment rule.
Within each day, request allocation order is authoritative. Original snapshot
and bar timestamps remain unchanged: the engine accepts different intraday times
on the same UTC date, so timestamps need not increase between stocks in a day.
Replay does not sort these timestamps or pretend that they are separate portfolio
valuation moments. One portfolio event provides the day's coherent closing state.

Incomplete groups, unexpected stock order, mismatched dates, duplicate/backward
dates, inconsistent daily accounts, disagreement with final account/positions,
and JSON-unsafe values fail with `invalid_result`. They are not silently repaired.
Dates missing from the source result cannot be invented: replay tests compare
every source snapshot against an independently written expected sequence.

## Values and ownership

Each snapshot retains bars, indicators, signals, positions, account values,
submitted orders and filled orders from the existing API DTO. Full-history mapping
must happen BEFORE timeline construction. Do not remap isolated snapshots: early
zero-volume indicator nulls, later accumulated indicators, valid numeric zeros,
and unavailable fields must retain their existing meaning.

The timeline stores encoded JSON privately. `Event(index)` returns a fresh copy,
so mutating an input response or another reader's event cannot change the stream.
Construction does not mutate the response. A failed constructor returns no partial
timeline. An out-of-range event lookup returns false.

## Playback timing

Speed means **historical daily indices per real second**, including non-Hold and
Hold days equally. The supported range is 0.25 through 20, with a
transport default of 1. Non-finite or out-of-range values fail explicitly with
`invalid_replay_speed`. The interval is one second divided by speed.

The first day is immediate. Transport waits only between daily groups; every
stock snapshot and the portfolio event for one day are sent together in sequence.
Start and final completion add no delay. This is intended playback pacing,
subject to actual network/client delivery; slow clients must never cause bars
to be skipped. `ReplayDayInterval` validates/calculates the interval but does not
sleep. The SSE transport implements scheduling, cancellation and backpressure.

Day indices and totals describe historical replay progress. They do not claim
progress inside the Monte Carlo computation or a live trading connection.

## Verification and next boundary

Task 1 tests use a deterministic engine fixture and a separately written event
sequence. They check exact counts, stock/day ordering, Hold updates, full snapshot
values and orders, coherent portfolio values, final completion, null-versus-zero
availability, input/reader ownership, malformed timelines, different intraday
timestamps and speed bounds.

Task 2 tests receive the SSE stream through an actual localhost HTTP client,
compare its order against an independently written sequence and its snapshots
against the completed result, and verify resume without rerunning the engine.
Other checks cover daily pacing, admission, invalid input, unavailable results,
byte/snapshot limits, disconnects, shutdown, maximum lifetime and job expiry.
Real TCP tests verify slow-reader write deadlines and replay across a server
write timeout shorter than a playback wait. These tests require localhost-bind
permission, but no provider credentials or external market-data access.

## SSE endpoint

```text
GET /api/backtests/{id}/replay?speed=1
Last-Event-ID: 4
```

`speed` is optional, fixed for one connection, and defaults to 1. Unknown query
parameters, repeated speed values, malformed numbers, and out-of-range speed
values return 400 before a stream starts. Only GET is accepted (other methods
return 405). A successful stream has `Content-Type: text/event-stream`,
`Cache-Control: no-store` and `X-Accel-Buffering: no`. Each event is flushed:

```text
id: 1
event: start
data: {"job_id":"example",...}

```

The data line contains the full JSON payload for that event, not the abbreviated
illustration above. The terminating blank line separates events. Source results
are copied and decoded from the job manager's cached bytes, preserving already
mapped availability. No provider fetch or Monte Carlo calculation runs for replay.

### Resume and completion

An absent cursor starts at event 1. `Last-Event-ID` is one nonnegative decimal
integer identifying the last event received for **this same job**. A connection
resumes at the next ID, even midway through a daily group; its first remaining
event is immediate. Subsequent new days use the selected playback interval.
There is no additional start event or automatic state reconstruction on resume:
the client must preserve prior events/state. A new client should start without
a cursor. Invalid or beyond-final cursors return 400; a cursor equal to the final
event returns 204 with no body.

Browser EventSource sends the last event ID when reconnecting. Clients must close
their EventSource after receiving `complete`. The 204 response also stops
reconnection after completion. A disconnect, write failure, shutdown or lifetime
limit closes the response without inventing a completion event. An EOF alone is
not successful replay completion; the client may resume while the job is retained.
See the [SSE specification](https://html.spec.whatwg.org/multipage/server-sent-events.html).

### Retention and errors

Queued, running and failed jobs return 409; unknown, expired or evicted jobs
return 404. An admitted replay owns a private timeline and can finish after the
source job expires. Expiry still removes the stored job normally. A new connection
or reconnect after expiry returns 404; the active stream does not make the job
durable. Process restart loses both jobs and streams.

Oversized source bytes, encoded timeline bytes or snapshot counts return 413.
Malformed/inconsistent stored results return a safe 500 before SSE headers.
Exhausted replay slots or server shutdown return 503; shared request rate
exhaustion returns 429. The existing JSON error envelope is used before streaming.
Unsupported response-writer flushing/deadline capabilities fail with 500 instead
of running an unbounded stream.

### Resource and timeout limits

| Setting | Default | Server flag |
| --- | --- | --- |
| Concurrent replay streams, including preparation | 2 | `-replay-connections` |
| Serialized source bytes and encoded timeline bytes, each per stream | 32 MiB | `-replay-max-bytes` |
| Write plus flush deadline per event | 5 seconds | `-replay-write-timeout` |
| Lifetime per connection, including preparation | 3 hours | `-replay-max-duration` |

Each connection receives a fresh lifetime beginning with replay admission. It is
not a timer for the application session or the underlying calculation. Default
stored-result retention is also three hours, measured from job completion, with
the existing 20-record/64-MiB budgets still permitting earlier eviction. Starting
another replay does not extend that stored-result deadline. An admitted stream
can continue after eviction, but a later reconnect cannot recover an evicted job.

The existing configured `-max-snapshots` budget also applies to replay. Serialized
byte budgets are not an exact bound on all decoded Go objects and temporary
encoding allocations; connection, source-byte and snapshot limits bound their
inputs. Source size is checked before copying, and the encoded timeline is checked
as it is built. No over-budget timeline is sent partially.

Replay slots are separate from the ordinary 16-handler budget, so long replay
connections do not occupy polling/submission slots. Replay connection attempts
still consume the shared request-rate budget. Synchronous write/flush provides
backpressure without an unbounded event queue. A slow reader hits its write
deadline and releases its slot; bars are never skipped to catch up.

The global 30-second response-write timeout is unchanged for ordinary requests.
Only an SSE response uses per-event write deadlines and clears that deadline
during deliberate playback waits, via Go's
[ResponseController](https://pkg.go.dev/net/http#ResponseController.SetWriteDeadline).
Disconnects and shutdown cancel waits; blocked writes remain bounded by their
deadline. A lifetime limit can end a long stream without completion, with a blocked
write taking up to its write timeout to return.

There is no separate heartbeat timer: at the slowest supported speed, deliberate
idle gaps are at most four seconds. Start/data frames provide activity, and blocked
writes fail on the deadline. This endpoint is for local single-user replay;
proxy/deployment behavior and a browser dashboard are not verified by these tests.

Phase-wide verification additionally checks HTTP/1.1 and HTTP/2 delivery,
independent reconstruction of cash and equity from streamed fills and positions,
equity changes during Hold, full-history zero-volume mapping, concurrent admission
and recovery while polling remains available, and separate three-hour connection
and result-retention behavior. The learning recap follows verification.
