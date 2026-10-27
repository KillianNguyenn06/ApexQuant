import test from 'node:test';
import assert from 'node:assert/strict';
import { aggregateFrames, filledMarkers, priceData } from '../assets/charts.mjs';
import { ReplayModel } from '../assets/replay.mjs';
import { detailsAt } from '../assets/details.mjs';
import { validateForm } from '../assets/form.mjs';
import { fixture } from './replay-fixture.mjs';

function minuteFixture() {
  const { result, events } = fixture();
  result.data.timeframe = '1Min'; result.request.timeframe = '1Min';
  for (const [i, s] of result.snapshots.entries()) {
    s.timestamp = s.bar.timestamp = `2026-01-05T14:${30 + Math.floor(i / 2)}:00Z`;
    s.bar.volume = 100; s.bar.vwap = s.bar.close;
  }
  const exit = { id: 'sell-1', symbol: 'AAPL', created_at: result.snapshots[2].timestamp, action: 'sell', status: 'filled', filled_price: 99, quantity: 1 };
  result.snapshots[2].filled_orders = [result.snapshots[2].filled_order, exit];
  result.snapshots[2].execution_reason = 'both_boundaries_stop_first';
  result.snapshots[4].decision_reason = 'buy_rejected_kelly';
  result.snapshots[4].risk = { win_probability: .3, loss_probability: .7, timeout_probability: 0, kelly_fraction: 0 };
  for (const event of events) {
    const data = JSON.parse(event.data);
    if (event.type === 'start') { data.request = result.request; data.data = result.data; }
    if (event.type === 'snapshot') data.snapshot = result.snapshots[data.snapshot_index];
    if (event.type === 'portfolio') data.date = result.snapshots[data.day_index * 2].timestamp;
    event.data = JSON.stringify(data);
  }
  return { result, events };
}

test('minute replay commits exact-time complete portfolios and retains same-minute fills', () => {
  const { result, events } = minuteFixture(), model = new ReplayModel(result);
  events.slice(0, 3).forEach(e => model.accept(e));
  assert.equal(model.frames.length, 0);
  events.slice(3).forEach(e => model.accept(e));
  assert.equal(model.frames.length, 3);
  assert.equal(model.frames[1].date, '2026-01-05T14:31:00Z');
  const detail = detailsAt(model, 2);
  assert.equal(detail.orders.filter(o => o.status === 'filled').length, 2);
  assert.equal(detail.events.find(e => e.reason === 'buy_rejected_kelly').risk.win_probability, .3);
});

test('5-minute and daily views preserve OHLC, volume and every fill without mutating execution', () => {
  const { result, events } = minuteFixture(), model = new ReplayModel(result); events.forEach(e => model.accept(e));
  const original = JSON.stringify(model.frames);
  for (const interval of ['5Min', '1Day']) {
    const grouped = aggregateFrames(model.frames, interval);
    assert.equal(grouped.length, 1);
    const series = priceData(grouped, 'AAPL'), b = series[0].snapshot.bar;
    assert.equal(b.open, 100); assert.equal(b.close, 99); assert.equal(b.high, 103); assert.equal(b.low, 98); assert.equal(b.volume, 300);
    assert.deepEqual(filledMarkers(series).map(m => m.action), ['buy', 'sell']);
    assert.equal(aggregateFrames(model.frames.slice(0, 1), interval)[0].snapshots[0].bar.close, 100);
  }
  assert.equal(JSON.stringify(model.frames), original);
});

test('intraday form accepts one day and counts minute workload', () => {
  const limits = { max_symbols: 8, percent_total_tolerance: .0001, earliest_start_date: '2017-01-01', latest_end_date: '2026-10-08', max_range_days: 365, max_snapshots: 390 };
  const input = { timeframe: '1Min', initial_capital: 50000, start_date: '2026-10-05', end_date: '2026-10-05', allocations: [{ symbol: 'AAPL', percent: 100 }] };
  assert.deepEqual(validateForm(input, limits).errors, {});
  assert.equal(validateForm(input, limits).request.timeframe, '1Min');
  assert.ok(validateForm(input, { ...limits, max_snapshots: 389 }).errors.allocations);
});
