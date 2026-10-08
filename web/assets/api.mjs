export class APIError extends Error {
  constructor(message, { code = 'connection_error', field = '', status = 0, retryAfter = 0 } = {}) {
    super(message);
    Object.assign(this, { code, field, status, retryAfter });
  }
}

export function createAPI({ fetchImpl = globalThis.fetch, timeout = 15000 } = {}) {
  async function request(path, { method = 'GET', body, signal } = {}) {
    const controller = new AbortController();
    const abort = () => controller.abort();
    if (signal?.aborted) controller.abort();
    signal?.addEventListener('abort', abort, { once: true });
    const timer = setTimeout(abort, timeout);
    try {
      const response = await fetchImpl(path, {
        method, signal: controller.signal, credentials: 'same-origin', cache: 'no-store',
        headers: { Accept: 'application/json', ...(body ? { 'Content-Type': 'application/json' } : {}) },
        ...(body ? { body: JSON.stringify(body) } : {}),
      });
      let data;
      try { data = await response.json(); }
      catch { throw new APIError('The server returned an unreadable response.', { code: 'invalid_response', status: response.status }); }
      if (!response.ok) {
        const retry = response.headers.get('Retry-After');
        const seconds = Number(retry);
        const retryAfter = retry ? (Number.isFinite(seconds) ? Math.max(0, seconds * 1000) : Math.max(0, Date.parse(retry) - Date.now())) : 0;
        throw new APIError(data?.error?.message || `Request failed (${response.status}).`, {
          code: data?.error?.code || 'request_error', field: data?.error?.field || '', status: response.status,
          retryAfter: Number.isFinite(retryAfter) ? retryAfter : 0,
        });
      }
      return data;
    } catch (error) {
      if (error instanceof APIError || signal?.aborted) throw error;
      throw new APIError(controller.signal.aborted ? 'The server took too long to respond.' : 'Cannot reach the local server. Check that it is running.');
    } finally {
      clearTimeout(timer);
      signal?.removeEventListener('abort', abort);
    }
  }
  return {
    config: signal => request('/api/config', { signal }),
    create: (body, signal) => request('/api/backtests', { method: 'POST', body, signal }),
    status: (id, signal) => request(`/api/backtests/${encodeURIComponent(id)}`, { signal }),
    result: (id, signal) => request(`/api/backtests/${encodeURIComponent(id)}/result`, { signal }),
  };
}

export function wait(milliseconds, signal) {
  return new Promise((resolve, reject) => {
    if (signal.aborted) return reject(new DOMException('Stopped', 'AbortError'));
    const abort = () => { clearTimeout(timer); reject(new DOMException('Stopped', 'AbortError')); };
    const timer = setTimeout(() => { signal.removeEventListener('abort', abort); resolve(); }, milliseconds);
    signal.addEventListener('abort', abort, { once: true });
  });
}

// Only polls calculation status. SSE playback and its progress belong to Task 2.
export class BacktestController {
  constructor(api, onChange, { delay = 2000, sleep = wait } = {}) {
    Object.assign(this, { api, onChange, delay, sleep, active: false, id: null, state: { phase: 'idle' } });
  }
  emit(state) { this.state = { ...this.state, ...state }; this.onChange(this.state); }
  async submit(request) {
    if (this.active) return;
    request = structuredClone(request);
    this.stop();
    this.active = true;
    this.id = null;
    this.abort = new AbortController();
    const signal = this.abort.signal;
    this.emit({ phase: 'submitting', request, id: null, result: null, error: null });
    try {
      const job = await this.api.create(request, signal);
      if (signal.aborted) return;
      if (typeof job?.id !== 'string' || !job.id || !['queued', 'running', 'completed'].includes(job.status)) throw new APIError('The server returned an invalid job.', { code: 'invalid_response' });
      this.id = job.id;
      this.emit({ phase: job.status === 'completed' ? 'loading_result' : job.status, id: job.id });
      await this.track(signal);
    } catch (error) {
      if (!signal.aborted) this.emit({ phase: this.id ? 'connection_lost' : 'submission_error', error,
        uncertainSubmission: !this.id && (!error.status || error.code === 'invalid_response' || (error.status >= 500 && !['capacity_unavailable', 'server_busy'].includes(error.code))) });
    } finally { if (this.abort.signal === signal) this.active = false; }
  }
  async track(signal) {
    while (!signal.aborted) {
      const job = await this.api.status(this.id, signal);
      if (signal.aborted) return;
      if (job?.id !== this.id || !['queued', 'running', 'completed', 'failed'].includes(job.status)) throw new APIError('The server returned an invalid status.', { code: 'invalid_response' });
      if (job.status === 'failed') { this.emit({ phase: 'failed', error: job.error || { message: 'Backtest could not be completed.' } }); return; }
      if (job.status === 'completed') {
        this.emit({ phase: 'loading_result' });
        const result = await this.api.result(this.id, signal);
        if (signal.aborted) return;
        if (result?.id !== this.id || !Array.isArray(result.snapshots) || !result.final_account) throw new APIError('The server returned an invalid result.', { code: 'invalid_response' });
        this.emit({ phase: 'completed', result, error: null });
        return;
      }
      this.emit({ phase: job.status, error: null });
      await this.sleep(this.delay, signal);
    }
  }
  async resume() {
    if (this.active || !this.id) return;
    this.active = true;
    this.abort = new AbortController();
    const signal = this.abort.signal;
    this.emit({ phase: 'checking', error: null });
    try { await this.track(signal); }
    catch (error) { if (!signal.aborted) this.emit({ phase: 'connection_lost', error }); }
    finally { if (this.abort.signal === signal) this.active = false; }
  }
  // Stopping browser tracking does not cancel server calculations.
  stop() { this.abort?.abort(); this.active = false; }
}
