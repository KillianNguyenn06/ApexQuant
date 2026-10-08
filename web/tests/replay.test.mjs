import test from 'node:test';
import assert from 'node:assert/strict';
import { APIError } from '../assets/api.mjs';
import { decodeSSE, streamReplay, ReplayModel, ReplayController } from '../assets/replay.mjs';
import { fixture } from './replay-fixture.mjs';

function body(chunks) { return new ReadableStream({ start(controller) { for (const chunk of chunks) controller.enqueue(chunk); controller.close(); } }); }
const bytes = text => new TextEncoder().encode(text);
const wire = events => events.map(e => `id: ${e.id}\nevent: ${e.type}\ndata: ${e.data}\n\n`).join('');
async function collect(stream, limit) { const result = []; for await (const event of decodeSSE(stream, limit)) result.push(event); return result; }

test('SSE decoder survives one-byte UTF-8 chunks, CRLF, comments, and multiline data', async () => {
  const text = ': comment\r\nid: 1\r\nevent: start\r\ndata: {"label":\r\ndata: "café"}\r\n\r\n';
  const chunks = [...bytes(text)].map(value => Uint8Array.of(value));
  assert.deepEqual(await collect(body(chunks)), [{ id: '1', type: 'start', data: '{"label":\n"café"}' }]);
});
test('SSE decoder rejects oversized and truncated events', async () => {
  await assert.rejects(collect(body([bytes('data: '+ 'x'.repeat(100))]), 30), /client limit/);
  await assert.rejects(collect(body([bytes('id: 1\ndata: {}\n')])), /during an event/);
});
test('wire events commit complete days in allocation order with every Hold value', () => {
  const { result, events } = fixture(), model = new ReplayModel(result);
  model.accept(events[0]); model.accept(events[1]);
  assert.equal(model.frames.length, 0); assert.equal(model.cursor, 1);
  model.accept(events[2]); assert.equal(model.frames.length, 0);
  for (const event of events.slice(3)) model.accept(event);
  assert.deepEqual(model.frames.map(frame => frame.date), ['2026-01-05', '2026-01-06', '2026-01-07']);
  assert.deepEqual(model.frames.map(frame => frame.snapshots.map(s => s.bar.symbol)), [['AAPL', 'MSFT'], ['AAPL', 'MSFT'], ['AAPL', 'MSFT']]);
  assert.deepEqual(model.frames.map(frame => frame.account.equity), [1000, 1001, 998]);
  assert.equal(model.frames[2].snapshots[0].signal.action, 'hold');
  assert.equal(model.frames[2].snapshots[0].indicator.standard_deviation, null);
  assert.equal(model.frames[2].snapshots[1].indicator.standard_deviation, 0);
  assert.equal(model.complete, true); assert.equal(model.cursor, 11);
});
test('model rejects skipped/repeated IDs, stock swaps, wrong values and premature completion', () => {
  for (const mutation of [
    e => ({ ...e, id: '3' }),
    e => ({ ...e, id: '1' }),
    e => ({ ...e, data: JSON.stringify({ ...JSON.parse(e.data), snapshot_index: 1 }) }),
    e => ({ ...e, data: JSON.stringify({ ...JSON.parse(e.data), snapshot: { ...JSON.parse(e.data).snapshot, bar: { symbol: 'MSFT' } } }) }),
    e => ({ ...e, data: 'null' }),
  ]) { const { result, events } = fixture(), model = new ReplayModel(result); model.accept(events[0]); assert.throws(() => model.accept(mutation(events[1])), /does not match/); }
  const { result, events } = fixture(), model = new ReplayModel(result); model.accept(events[0]);
  assert.throws(() => model.accept({ ...events.at(-1), id: '2' }), /does not match/);
});
test('partial day is rolled back and retried after the last complete portfolio event', () => {
  const { result, events } = fixture(), model = new ReplayModel(result);
  for (const event of events.slice(0, 5)) model.accept(event);
  assert.equal(model.cursor, 4); assert.equal(model.pending, 1);
  model.rollback(); assert.equal(model.nextID, 5); assert.equal(model.pending, 0);
  for (const event of events.slice(4)) model.accept(event);
  assert.equal(model.frames.length, 3); assert.equal(model.complete, true);
});
test('HTTP stream sends the cursor and speed, stops on complete, and surfaces expiry', async () => {
  const { result, events } = fixture(), model = new ReplayModel(result);
  events.slice(0, 4).forEach(event => model.accept(event));
  let seen;
  await streamReplay('fixture', 4, 2, new AbortController().signal, event => model.accept(event) === 'complete', { fetchImpl: async (path, options) => {
    seen = { path, options }; return new Response(body([bytes(wire(events.slice(4)))]), { headers: { 'Content-Type': 'text/event-stream' } });
  }});
  assert.equal(seen.path, '/api/backtests/fixture/replay?speed=2'); assert.equal(seen.options.headers['Last-Event-ID'], '4');
  assert.equal(model.complete, true);
  await assert.rejects(streamReplay('fixture', 4, 1, new AbortController().signal, () => false, { fetchImpl: async () => new Response(JSON.stringify({ error: { message: 'Expired', code: 'job_not_found' } }), { status: 404 }) }), error => error.status === 404 && error.message === 'Expired');
});
test('interrupted stream resumes exactly once per day; cached restart does not reconnect', async () => {
  const { result, events } = fixture(); let calls = 0; const cursors = [];
  const controller = new ReplayController(() => {}, { sleep: async () => {}, stream: async (_id, cursor, _speed, _signal, consume) => {
    cursors.push(cursor);
    if (calls++ === 0) { events.slice(0, 5).forEach(consume); throw new APIError('Offline'); }
    for (const event of events.slice(4)) if (consume(event)) break;
  }});
  controller.reset(result); await controller.play();
  assert.equal(controller.mode, 'interrupted'); assert.equal(controller.model.cursor, 4); assert.equal(controller.model.frames.length, 1);
  await controller.play(); assert.deepEqual(cursors, [0, 4]); assert.equal(controller.mode, 'completed');
  await controller.restart(); assert.equal(calls, 2); assert.equal(controller.view, 2);
  controller.previous(); assert.equal(controller.view, 1); await controller.next(); assert.equal(controller.view, 2); assert.equal(controller.mode, 'completed');
});
test('single-step stops after one complete day even when more events arrived in the same chunk', async () => {
  const { result, events } = fixture(); const cursorValues = [];
  const controller = new ReplayController(() => {}, { stream: async (_id, cursor, _speed, _signal, consume) => {
    cursorValues.push(cursor); for (const event of events.filter(e => Number(e.id) > cursor)) if (consume(event)) break;
  }});
  controller.reset(result); await controller.next(); assert.equal(controller.model.frames.length, 1); assert.equal(controller.mode, 'paused');
  await controller.next(); assert.deepEqual(cursorValues, [0, 4]); assert.equal(controller.model.frames.length, 2);
});
test('pause cancels the connection and ignores late events from an old connection', async () => {
  const { result, events } = fixture(); let deliver, signal;
  const controller = new ReplayController(() => {}, { stream: async (_id, _cursor, _speed, suppliedSignal, consume) => { signal = suppliedSignal; deliver = consume; } });
  controller.reset(result); await controller.play();
  controller.pause(); assert.equal(signal.aborted, true); assert.equal(deliver(events[0]), true); assert.equal(controller.model.cursor, 0);
});

test('speed change cancels a partial day and resumes from its preceding committed cursor', async () => {
  const { result, events } = fixture(); const requests = []; let late;
  const controller = new ReplayController(() => {}, { stream: async (_id, cursor, speed, signal, consume) => {
    requests.push({ cursor, speed, signal });
    if (requests.length === 1) { events.slice(0, 5).forEach(consume); late = consume; return; }
    for (const event of events.slice(4)) if (consume(event)) break;
  }});
  controller.reset(result); await controller.play();
  assert.equal(controller.model.pending, 1);
  await controller.setSpeed(10);
  assert.equal(requests[0].signal.aborted, true);
  assert.deepEqual(requests.map(({cursor, speed}) => ({cursor, speed})), [{cursor:0, speed:1}, {cursor:4, speed:10}]);
  assert.equal(late(events[5]), true);
  assert.equal(controller.model.frames.length, 3); assert.equal(controller.mode, 'completed');
  assert.throws(() => controller.setSpeed(0), /Invalid/);
});

test('HTTP idle deadline interrupts a stalled connection without claiming completion', async () => {
  await assert.rejects(streamReplay('fixture', 0, 1, new AbortController().signal, () => false, {
    idleTimeout: 5,
    fetchImpl: async (_path, {signal}) => new Promise((_resolve, reject) => signal.addEventListener('abort', () => reject(new Error('Aborted')), {once:true})),
  }), /stopped responding/);
});
