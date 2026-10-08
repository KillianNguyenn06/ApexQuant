import { APIError, wait } from './api.mjs';

const MAX_EVENT = 1024 * 1024;
const invalid = () => new APIError('Replay data does not match this completed backtest.', { code: 'invalid_replay' });
function equal(a, b) {
  if (a === b) return true;
  if (!a || !b || typeof a !== 'object' || typeof b !== 'object' || Array.isArray(a) !== Array.isArray(b)) return false;
  const keys = Object.keys(a);
  return keys.length === Object.keys(b).length && keys.every(key => Object.hasOwn(b, key) && equal(a[key], b[key]));
}

// Incremental SSE framing: handles UTF-8, split CRLF, comments and multiline data.
// A truncated frame is rejected rather than treated as a completed event.
export async function* decodeSSE(body, maxEvent = MAX_EVENT, onChunk = () => {}) {
  const reader = body.getReader(), decoder = new TextDecoder('utf-8', { fatal: true });
  let buffer = '', fields = { data: [] }, size = 0;
  function line(value) {
    size += value.length + 1;
    if (size > maxEvent) throw new APIError('A replay event exceeded the client limit.', { code: 'replay_too_large' });
    if (value === '') {
      const event = fields.data.length ? { id: fields.id, type: fields.event || 'message', data: fields.data.join('\n') } : null;
      fields = { data: [] }; size = 0; return event;
    }
    if (value.startsWith(':')) return null;
    const colon = value.indexOf(':'), key = colon < 0 ? value : value.slice(0, colon);
    let text = colon < 0 ? '' : value.slice(colon + 1);
    if (text.startsWith(' ')) text = text.slice(1);
    if (key === 'data') fields.data.push(text);
    else if (key === 'id' || key === 'event') fields[key] = text;
    return null;
  }
  try {
    while (true) {
      const { value, done } = await reader.read();
      if (!done) onChunk();
      buffer += done ? decoder.decode() : decoder.decode(value, { stream: true });
      let offset;
      while ((offset = buffer.search(/[\r\n]/)) >= 0) {
        if (!done && buffer[offset] === '\r' && offset === buffer.length - 1) break;
        const width = buffer[offset] === '\r' && buffer[offset + 1] === '\n' ? 2 : 1;
        const event = line(buffer.slice(0, offset)); buffer = buffer.slice(offset + width);
        if (event) yield event;
      }
      if (buffer.length + size > maxEvent) throw new APIError('A replay event exceeded the client limit.', { code: 'replay_too_large' });
      if (done) {
        if (buffer || fields.data.length) throw new APIError('The replay ended during an event. Resume to retry this day.', { code: 'replay_interrupted' });
        return;
      }
    }
  } finally {
    await reader.cancel().catch(() => {});
    reader.releaseLock();
  }
}

export async function streamReplay(id, cursor, speed, signal, consume, { fetchImpl = globalThis.fetch, idleTimeout = 20000 } = {}) {
  const abort = new AbortController();
  const cancel = () => abort.abort();
  if (signal.aborted) abort.abort();
  signal.addEventListener('abort', cancel, { once: true });
  let timer;
  const refresh = () => { clearTimeout(timer); timer = setTimeout(cancel, idleTimeout); };
  refresh();
  try {
    const response = await fetchImpl(`/api/backtests/${encodeURIComponent(id)}/replay?speed=${speed}`, {
      signal: abort.signal, credentials: 'same-origin', cache: 'no-store',
      headers: { Accept: 'text/event-stream', ...(cursor ? { 'Last-Event-ID': String(cursor) } : {}) },
    });
    if (!response.ok) {
      const data = await response.json().catch(() => null);
      throw new APIError(data?.error?.message || `Replay unavailable (${response.status}).`, { status: response.status, code: data?.error?.code || 'replay_unavailable' });
    }
    if (response.status === 204 || !response.body || !response.headers.get('Content-Type')?.startsWith('text/event-stream')) throw invalid();
    // Reset the idle deadline on received bytes, including comments/partial frames.
    for await (const event of decodeSSE(response.body, MAX_EVENT, refresh)) {
      if (signal.aborted) return;
      if (consume(event)) return;
    }
    throw new APIError('Replay connection ended before completion. Resume to continue.', { code: 'replay_interrupted' });
  } catch (error) {
    if (error instanceof APIError || signal.aborted) throw error;
    throw new APIError(abort.signal.aborted ? 'Replay stopped responding. Resume to continue.' : 'Replay connection interrupted. Resume to continue.', { code: 'replay_interrupted' });
  } finally { cancel(); clearTimeout(timer); signal.removeEventListener('abort', cancel); }
}

// Completed HTTP results are the value oracle. Only SSE-confirmed daily groups
// enter displayed history; pending partial days never change the displayed frame.
export class ReplayModel {
  constructor(result) {
    if (!result || !Array.isArray(result.request?.allocations) || !Array.isArray(result.snapshots)) throw invalid();
    this.result = result;
    this.symbols = result.request.allocations.map(row => row.symbol);
    if (!result.id || this.symbols.length < 1 || this.symbols.length > 8 || new Set(this.symbols).size !== this.symbols.length || !result.snapshots.length || result.snapshots.length % this.symbols.length) throw invalid();
    this.totalDays = result.snapshots.length / this.symbols.length;
    this.frames = []; this.cursor = 0; this.nextID = 1; this.pending = 0; this.complete = false;
  }
  rollback() { this.pending = 0; this.nextID = this.cursor + 1; }
  accept(event) {
    if (!/^[1-9]\d*$/.test(String(event.id)) || !Number.isSafeInteger(Number(event.id)) || Number(event.id) !== this.nextID || this.complete) throw invalid();
    let data;
    try { data = JSON.parse(event.data); } catch { throw invalid(); }
    if (!data || typeof data !== 'object') throw invalid();
    const n = this.symbols.length, day = this.frames.length, base = day * n;
    switch (event.type) {
      case 'start':
        if (this.cursor || data.job_id !== this.result.id || data.total_days !== this.totalDays || data.total_snapshots !== this.result.snapshots.length || !equal(data.request, this.result.request) || !equal(data.settings, this.result.settings) || !equal(data.data, this.result.data)) throw invalid();
        this.cursor = Number(event.id); break;
      case 'snapshot':
        if (!this.cursor || day >= this.totalDays || this.pending >= n || data.day_index !== day || data.snapshot_index !== base + this.pending || !equal(data.snapshot, this.result.snapshots[base + this.pending])) throw invalid();
        this.pending++; break;
      case 'portfolio': {
        if (this.pending !== n || data.day_index !== day) throw invalid();
        const snapshots = this.result.snapshots.slice(base, base + n);
        const date = new Date(snapshots[0].timestamp).toISOString().slice(0, 10);
        const positions = Object.fromEntries(snapshots.map(s => [s.bar.symbol, s.position]));
        if (snapshots.some((s, i) => s.bar.symbol !== this.symbols[i] || new Date(s.timestamp).toISOString().slice(0, 10) !== date || !equal(s.account, data.account)) || (day && date <= this.frames[day - 1].date) || data.date !== date || !equal(data.positions, positions)) throw invalid();
        this.frames.push({ date, snapshots, account: snapshots[0].account, positions });
        this.pending = 0; this.cursor = Number(event.id); break;
      }
      case 'complete':
        if (this.pending || day !== this.totalDays || data.job_id !== this.result.id || data.total_days !== this.totalDays || data.total_snapshots !== this.result.snapshots.length || !equal(data.final_account, this.result.final_account) || !equal(data.final_positions, this.result.final_positions) || !equal(data.final_account, this.frames[day - 1].account) || !equal(data.final_positions, this.frames[day - 1].positions)) throw invalid();
        this.complete = true; this.cursor = Number(event.id); break;
      default: throw invalid();
    }
    this.nextID++;
    return event.type;
  }
}

export class ReplayController {
  constructor(onChange, { stream = streamReplay, sleep = wait } = {}) {
    Object.assign(this, { onChange, stream, sleep, speed: 1, mode: 'idle', model: null, view: -1, error: null });
  }
  emit() { this.onChange(this); }
  reset(result = null) {
    this.pause(false); this.model = result ? new ReplayModel(result) : null;
    this.view = -1; this.error = null; this.mode = result ? 'ready' : 'idle'; this.emit();
  }
  pause(emit = true) {
    this.abort?.abort(); this.model?.rollback();
    if (this.model) this.mode = 'paused';
    if (emit) this.emit();
  }
  async play({ step = false } = {}) {
    if (!this.model || this.mode === 'playing') return;
    const model = this.model;
    this.abort = new AbortController(); const signal = this.abort.signal;
    this.mode = 'playing'; this.error = null; this.emit();
    try {
      // Previously received history is replayed locally at the same day speed.
      while (!signal.aborted && this.view + 1 < model.frames.length) {
        this.view++; this.emit();
        if ((step || model.complete) && this.view === model.frames.length - 1) { this.mode = model.complete ? 'completed' : 'paused'; this.emit(); return; }
        if (step) { this.mode = 'paused'; this.emit(); return; }
        await this.sleep(1000 / this.speed, signal);
      }
      if (signal.aborted) return;
      if (model.complete) { this.mode = 'completed'; this.emit(); return; }
      await this.stream(model.result.id, model.cursor, this.speed, signal, event => {
        if (signal.aborted) return true;
        const type = model.accept(event);
        if (type === 'portfolio') {
          this.view = model.frames.length - 1; this.emit();
          if (step) { this.pause(); return true; }
        }
        if (type === 'complete') { this.mode = 'completed'; this.emit(); return true; }
        return false;
      });
    } catch (error) {
      if (!signal.aborted && this.model === model) { model.rollback(); this.mode = 'interrupted'; this.error = error; this.emit(); }
    }
  }
  previous() { if (!this.model) return; this.pause(false); this.view = Math.max(-1, this.view - 1); this.emit(); }
  next() { this.pause(false); return this.play({ step: true }); }
  seek(index) { if (!this.model || !Number.isFinite(index)) return; this.pause(false); this.view = Math.max(-1, Math.min(Math.trunc(index), this.model.frames.length - 1)); this.emit(); }
  restart() { if (!this.model) return; this.pause(false); this.view = -1; return this.play(); }
  setSpeed(value) {
    if (!Number.isFinite(value) || value < .25 || value > 20) throw new Error('Invalid playback speed');
    const playing = this.mode === 'playing'; this.pause(false); this.speed = value;
    if (playing) return this.play();
    this.emit();
  }
}
