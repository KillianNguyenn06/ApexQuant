import test from 'node:test';
import assert from 'node:assert/strict';
import { APIError, createAPI, BacktestController } from '../assets/api.mjs';

test('API uses only local endpoints and preserves structured server errors', async () => {
  let seen;
  const api = createAPI({ fetchImpl: async (path, options) => {
    seen = { path, options };
    return new Response(JSON.stringify({ error: { code: 'rate_limited', message: 'Wait before retrying.', field: 'allocations' } }), { status: 429, headers: { 'Retry-After': '60' } });
  }});
  await assert.rejects(api.create({ initial_capital: 100 }), error => error instanceof APIError && error.status === 429 && error.retryAfter === 60000 && error.field === 'allocations');
  assert.equal(seen.path, '/api/backtests');
  assert.equal(seen.options.method, 'POST');
  assert.equal(seen.options.credentials, 'same-origin');
  assert.equal(seen.options.headers['Content-Type'], 'application/json');
});
test('request timeout aborts a hung connection', async () => {
  const api = createAPI({ timeout: 5, fetchImpl: (_path, { signal }) => new Promise((_resolve, reject) => signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))) });
  await assert.rejects(api.config(), /too long/);
});
test('controller follows queued/running/completed and fetches the matching result once', async () => {
  const phases = []; let creates = 0, results = 0, i = 0;
  const api = {
    create: async () => { creates++; return { id: 'job', status: 'queued' }; },
    status: async () => ({ id: 'job', status: ['queued', 'running', 'completed'][i++] }),
    result: async () => { results++; return { id: 'job', snapshots: [], final_account: { cash: 100 } }; },
  };
  const controller = new BacktestController(api, state => phases.push(state.phase), { sleep: async () => {} });
  const run = controller.submit({ initial_capital: 100 });
  await controller.submit({ initial_capital: 200 }); // Duplicate click is ignored.
  await run;
  assert.deepEqual(phases, ['submitting', 'queued', 'queued', 'running', 'loading_result', 'completed']);
  assert.equal(creates, 1); assert.equal(results, 1); assert.equal(controller.active, false);
});
test('failed calculation ends tracking without requesting a result', async () => {
  const controller = new BacktestController({
    create: async () => ({ id: 'job', status: 'queued' }),
    status: async () => ({ id: 'job', status: 'failed', error: { message: 'Data unavailable.' } }),
    result: async () => assert.fail('must not fetch failed result'),
  }, () => {});
  await controller.submit({});
  assert.equal(controller.state.phase, 'failed');
  assert.equal(controller.state.error.message, 'Data unavailable.');
});
test('lost tracking can resume without creating another calculation', async () => {
  let creates = 0, statuses = 0;
  const controller = new BacktestController({
    create: async () => { creates++; return { id: 'job', status: 'running' }; },
    status: async () => { if (statuses++ === 0) throw new APIError('Offline'); return { id: 'job', status: 'completed' }; },
    result: async () => ({ id: 'job', snapshots: [], final_account: {} }),
  }, () => {});
  await controller.submit({});
  assert.equal(controller.state.phase, 'connection_lost');
  await controller.resume();
  assert.equal(controller.state.phase, 'completed'); assert.equal(creates, 1);
});
test('uncertain submission is not retried automatically', async () => {
  let creates = 0;
  const controller = new BacktestController({ create: async () => { creates++; throw new APIError('Offline'); } }, () => {});
  await controller.submit({});
  assert.equal(controller.state.phase, 'submission_error'); assert.equal(controller.state.uncertainSubmission, true);
  await controller.resume(); assert.equal(creates, 1);
});
test('server errors distinguish uncertain acceptance from a rejected queue submission', async () => {
  for (const [code, uncertain] of [['internal_error', true], ['capacity_unavailable', false]]) {
    const controller = new BacktestController({ create: async () => { throw new APIError('Unavailable', { status: 503, code }); } }, () => {});
    await controller.submit({});
    assert.equal(controller.state.uncertainSubmission, uncertain);
  }
});
test('stopping tracking ignores late responses and does not send a server cancellation', async () => {
  let finish;
  const phases = [];
  const controller = new BacktestController({ create: () => new Promise(resolve => { finish = resolve; }) }, state => phases.push(state.phase));
  const run = controller.submit({});
  controller.stop();
  finish({ id: 'job', status: 'running' });
  await run;
  assert.deepEqual(phases, ['submitting']);
});

test('immediately completed submission loads its result before announcing completion', async () => {
  const controller = new BacktestController({
    create: async () => ({ id: 'job', status: 'completed' }),
    status: async () => ({ id: 'job', status: 'completed' }),
    result: async () => ({ id: 'job', snapshots: [], final_account: { equity: 1000 } }),
  }, state => { if (state.phase === 'completed') assert.equal(state.result.final_account.equity, 1000); });
  await controller.submit({});
  assert.equal(controller.state.phase, 'completed');
});
