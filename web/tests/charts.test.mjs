import test from 'node:test';
import assert from 'node:assert/strict';
import { priceData, filledMarkers, linePaths, valueBounds } from '../assets/charts.mjs';
import { ReplayModel } from '../assets/replay.mjs';
import { fixture } from './replay-fixture.mjs';

test('chart series preserves Hold days and selects the correct stock and visible window', () => {
  const { result, events } = fixture(), model = new ReplayModel(result); events.forEach(e => model.accept(e));
  const series = priceData(model.frames, 'AAPL');
  assert.deepEqual(series.map(row => row.snapshot.bar.close), [100, 102, 99]);
  assert.deepEqual(priceData(model.frames, 'MSFT', 2).map(row => row.snapshot.bar.close), [201, 202]);
  assert.deepEqual(series.map(row => row.date), ['2026-01-05', '2026-01-06', '2026-01-07']);
});
test('trade markers use filled orders, not submitted orders or opportunity signals', () => {
  const { result, events } = fixture(), model = new ReplayModel(result); events.forEach(e => model.accept(e));
  const series = priceData(model.frames, 'AAPL');
  assert.deepEqual(filledMarkers(series), [{ index: 1, action: 'buy', price: 101, quantity: 1 }]);
  series[2].snapshot.filled_order = { action: 'sell', status: 'filled', filled_price: 100, quantity: 1 };
  assert.equal(filledMarkers(series)[1].action, 'sell');
});
test('indicator paths break at unavailable values and retain real numeric zero', () => {
  assert.deepEqual(linePaths([null, 0, 2, null, 3], i => i, value => value), ['M1.00,0.00 L2.00,2.00', 'M4.00,3.00']);
  assert.deepEqual(linePaths([null, null], i => i, value => value), []);
});
test('flat chart values receive a nonzero scale without inventing data', () => {
  const [lo, hi] = valueBounds([100, 100, null]); assert.ok(lo < 100 && hi > 100);
  assert.deepEqual(valueBounds([null]), [0, 1]);
});

test('equity inspection clamps to displayed days and returns capital-relative gains', async () => {
  const { equityIndex, equityReturn } = await import('../assets/charts.mjs');
  assert.equal(equityIndex(50, 0, 100, 0, 2, 3), 1);
  assert.equal(equityIndex(-20, 0, 100, 0, 2, 3), 0);
  assert.equal(equityIndex(120, 0, 100, 0, 2, 3), 2);
  assert.equal(equityIndex(50, 0, 100, -.5, .5, 1), 0);
  assert.deepEqual(equityReturn(10021.81, 10000), { gain: 10021.81 - 10000, percent: (10021.81 - 10000) / 10000 * 100 });
  assert.deepEqual(equityReturn(900, 1000), { gain: -100, percent: -10 });
});
