import { createAPI, BacktestController } from './api.mjs';
import { dateValue, shiftDate, validateForm } from './form.mjs';
import { ReplayController } from './replay.mjs';
import { drawPrice, drawEquity, resetChart, equityReturn } from './charts.mjs';
import { motionDuration } from './motion.mjs';
import { detailsAt, pageRows, runConfiguration } from './details.mjs';

const $ = id => document.getElementById(id);
const api = createAPI();
const money = value => Number.isFinite(value) ? new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', maximumFractionDigits: 2 }).format(Math.abs(value) < .005 ? 0 : value) : '—';
let limits, settings, busy = false;
const dialog = $('settings-dialog');
const replay = new ReplayController(renderReplay);
const controller = new BacktestController(api, renderState);
let replayJob = null, chartSymbol = '', lastReplayMode = '', chartModel = null;
let orderPage = 0, eventPage = 0, detailModel = null, detailView = -2, detailState;
let painted = null, stockPage = 0, allocationDraft = null;
const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)');
const quantity = value => Number.isFinite(value) ? new Intl.NumberFormat('en-US', { maximumFractionDigits: 6 }).format(value) : '—';
const percent = value => Number.isFinite(value) ? `${value.toFixed(2)}%` : '—';
const orderDate = value => value && Number.isFinite(Date.parse(value)) ? new Date(value).toISOString().slice(0, 10) : '—';

function rows() {
  return [...$('allocation-rows').children].map(row => ({ symbol: row.querySelector('[data-symbol]').value, percent: row.querySelector('[data-percent]').value }));
}
function addRow(symbol = '', percent = '') {
  const row = document.createElement('div'); row.className = 'allocation-row';
  const ticker = document.createElement('input');
  Object.assign(ticker, { type: 'text', value: symbol, maxLength: 10, autocomplete: 'off', spellcheck: false });
  ticker.dataset.symbol = ''; ticker.setAttribute('aria-label', 'Stock symbol');
  const weight = document.createElement('input');
  Object.assign(weight, { type: 'number', value: String(percent), min: '0', max: '100', step: 'any', inputMode: 'decimal' });
  weight.dataset.percent = ''; weight.setAttribute('aria-label', 'Allocation percentage');
  const remove = document.createElement('button'); remove.type = 'button'; remove.textContent = '×'; remove.setAttribute('aria-label', 'Remove stock');
  remove.addEventListener('click', () => { row.remove(); updateTotal(); });
  ticker.addEventListener('input', clearErrors); weight.addEventListener('input', updateTotal);
  ticker.addEventListener('blur', () => { ticker.value = ticker.value.trim().toUpperCase(); });
  row.append(ticker, weight, remove); $('allocation-rows').append(row); updateTotal();
}
function updateTotal() {
  const allocations = rows();
  const total = allocations.reduce((sum, row) => sum + Number(row.percent), 0);
  const valid = Number.isFinite(total) && Math.abs(total - 100) <= (limits?.percent_total_tolerance ?? .0001);
  $('allocation-total').textContent = Number.isFinite(total) ? `${Number(total.toFixed(4))}%` : '—';
  $('allocation-total').classList.toggle('invalid', !valid);
  $('add-stock').disabled = allocations.length >= (limits?.max_symbols ?? 8);
  [...$('allocation-rows').children].forEach(row => row.querySelector('button').disabled = allocations.length <= 1);
  clearErrors();
}
function clearErrors() {
  $('allocation-error').textContent = '';
  document.querySelectorAll('[aria-invalid]').forEach(input => input.removeAttribute('aria-invalid'));
}
function setSummary() {
  $('capital-summary').textContent = money(Number(settings.initial_capital));
  $('date-summary').textContent = `${settings.start_date} → ${settings.end_date}`;
}
function readSettings() {
  return { initial_capital: $('initial-capital').value, start_date: $('start-date').value, end_date: $('end-date').value };
}
function populateSettings() {
  $('initial-capital').value = settings.initial_capital;
  $('start-date').value = settings.start_date; $('end-date').value = settings.end_date;
  $('period-preset').value = settings.preset || 'custom';
  ['capital-error', 'start-error', 'end-error'].forEach(id => $(id).textContent = '');
}
function showSettings() {
  if (!settings || busy) return;
  allocationDraft = rows(); populateSettings(); dialog.showModal();
}
function inputFor(field) {
  const ids = { initial_capital: 'initial-capital', start_date: 'start-date', end_date: 'end-date' };
  if (ids[field]) return $(ids[field]);
  const match = /^allocations\[(\d+)\]\.(symbol|percent)$/.exec(field);
  return match ? $('allocation-rows').children[Number(match[1])]?.querySelector(match[2] === 'symbol' ? '[data-symbol]' : '[data-percent]') : null;
}
function showErrors(errors) {
  clearErrors();
  const settingFields = { initial_capital: 'capital-error', start_date: 'start-error', end_date: 'end-error' };
  const messages = [];
  for (const [field, message] of Object.entries(errors)) {
    inputFor(field)?.setAttribute('aria-invalid', 'true');
    if (settingFields[field]) $(settingFields[field]).textContent = message;
    else messages.push(message);
  }
  $('allocation-error').textContent = [...new Set(messages)].join(' ');
}
function log(message) {
  $('run-log').querySelector('.log-empty')?.remove();
  const entry = document.createElement('li'), time = document.createElement('time'), text = document.createElement('span');
  const now = new Date(); time.dateTime = now.toISOString(); time.textContent = now.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
  text.textContent = message; entry.append(time, text); $('run-log').prepend(entry);
  while ($('run-log').children.length > 60) $('run-log').lastChild.remove();
}

let lastPhase = '';
function renderState(state) {
  if (state.phase === 'submitting') { replay.reset(); replayJob = null; }
  busy = ['submitting', 'queued', 'running', 'checking', 'loading_result'].includes(state.phase);
  $('portfolio-fields').disabled = busy || !limits;
  $('run-button').disabled = busy || !limits; $('settings-fields').disabled = busy || !limits;
  $('settings-open').disabled = busy || !limits; $('settings-edit').disabled = busy || !limits;
  $('run-button').firstChild.textContent = busy ? 'Backtest in progress ' : (state.id ? 'Run another backtest ' : 'Run backtest ');
  const text = {
    submitting: ['Submitting backtest', 'Sending your portfolio to the local server…'],
    queued: ['Waiting to start', 'Your backtest is queued. It will start when a calculation slot is available.'],
    running: ['Calculating backtest', 'Processing historical prices. Calculation progress is not available yet.'],
    checking: ['Checking your run', 'Reconnecting to the existing backtest…'],
    loading_result: ['Preparing results', 'Calculation finished. Loading its recorded history…'],
    completed: ['Backtest complete', 'Your historical results are ready. The figures below describe the completed run.'],
    failed: ['Backtest failed', 'Review the error before starting another run.'],
    connection_lost: [state.error?.status === 404 ? 'Run unavailable' : 'Connection interrupted', state.error?.status === 404 ? 'This run may have expired or the server may have restarted.' : 'Tracking stopped. Reconnect to check the same run without submitting it again.'],
    submission_error: ['Submission interrupted', state.uncertainSubmission ? 'The server may have accepted this run. It was not retried automatically; submitting again may start another calculation.' : 'Correct the problem and try again.'],
  }[state.phase] || ['Ready when you are', 'Configure your portfolio, then run a historical backtest.'];
  $('progress-title').textContent = text[0]; $('status-detail').textContent = text[1];
  $('top-status').textContent = `● ${text[0]}`;
  $('status-dot').classList.toggle('busy', busy);
  $('status-dot').classList.toggle('failed', ['failed', 'connection_lost', 'submission_error'].includes(state.phase));
  $('run-error').hidden = !state.error;
  $('run-error').textContent = state.error?.message || '';
  $('retry-button').hidden = state.phase !== 'connection_lost' || state.error?.status === 404;
  $('result-summary').hidden = state.phase !== 'completed';
  if (state.phase === 'submitting') { $('run-log').replaceChildren(); lastPhase = ''; }
  if (state.phase !== lastPhase) log(text[0]);
  lastPhase = state.phase;
  if (state.error?.field) showErrors({ [state.error.field]: state.error.message });
  if (state.phase === 'completed') {
    $('final-equity').textContent = money(state.result.final_account.equity);
    $('final-cash').textContent = money(state.result.final_account.cash);
    $('snapshot-count').textContent = state.result.snapshots.length.toLocaleString();
    if (replayJob !== state.id) {
      replayJob = state.id;
      try { replay.reset(state.result); void replay.play(); }
      catch (error) { $('replay-error').hidden = false; $('replay-error').textContent = error.message; }
    }
  }
}

function renderCharts({ animate = false, force = false } = {}) {
  const signature = { model: replay.model, view: replay.view, symbol: chartSymbol, priceWidth: $('price-chart-wrap').clientWidth, equityWidth: $('equity-chart-wrap').clientWidth };
  if (!force && painted && Object.keys(signature).every(key => signature[key] === painted[key])) return;
  const sameContext = painted && signature.model === painted.model && signature.symbol === painted.symbol && signature.priceWidth === painted.priceWidth && signature.equityWidth === painted.equityWidth;
  const duration = animate && sameContext && signature.view === painted.view + 1 && $('replay-smooth').checked && !reducedMotion.matches ? motionDuration(replay.speed) : 0;
  painted = signature;
  const frames = replay.model?.frames.slice(0, replay.view + 1) || [];
  const current = frames.at(-1);
  $('price-empty').hidden = !!current; $('price-chart-wrap').hidden = !current;
  $('equity-empty').hidden = !!current; $('equity-chart-wrap').hidden = !current;
  $('price-value').textContent = '—'; $('vwap-value').textContent = '—'; $('equity-value').textContent = '';
  $('equity-caption').textContent = 'Cash + market value of holdings';
  $('price-caption').textContent = 'Historical prices · Every trading day';
  $('equity-return').textContent = '';
  if (!current) { resetChart($('price-chart')); resetChart($('equity-chart')); return; }
  const snapshot = current.snapshots.find(s => s.bar.symbol === chartSymbol);
  const count = drawPrice($('price-chart'), frames, chartSymbol, duration);
  drawEquity($('equity-chart'), frames, duration, frame => {
    if (!frame) { $('equity-readout').textContent = 'Hover over the chart, or use arrow keys, to inspect a trading day.'; return; }
    const change = equityReturn(frame.account.equity, replay.model.result.request.initial_capital);
    $('equity-readout').textContent = `${frame.date} · Equity ${money(frame.account.equity)} · ${signedMoney(change.gain)} (${signedPercent(change.percent)})`;
  });
  $('equity-readout').textContent = 'Hover over the chart, or use arrow keys, to inspect a trading day.';
  $('price-value').textContent = snapshot.bar.close.toFixed(2);
  $('vwap-value').textContent = snapshot.indicator.vwap === null ? '—' : snapshot.indicator.vwap.toFixed(2);
  $('equity-value').textContent = money(current.account.equity);
  const change = equityReturn(current.account.equity, replay.model.result.request.initial_capital);
  $('equity-return').textContent = `${signedMoney(change.gain)} (${signedPercent(change.percent)})`;
  tone($('equity-return'), change.gain);
  $('equity-caption').textContent = `Historical day · ${current.date}`;
  $('price-caption').textContent = `${current.date} · Showing latest ${count} of ${frames.length} days`;
}

function renderReplay(state) {
  const model = state.model, playing = state.mode === 'playing';
  if (model !== chartModel) {
    chartModel = model; $('chart-symbol').replaceChildren();
    chartSymbol = model?.symbols[0] || '';
    for (const symbol of model?.symbols || []) { const option = document.createElement('option'); option.value = symbol; option.textContent = symbol; $('chart-symbol').append(option); }
    $('price-title').textContent = model ? 'Daily price & indicators' : 'Price & indicators';
  }
  $('chart-symbol').disabled = !model;
  const modeText = { idle: 'Awaiting a completed run', ready: 'Ready', playing: 'Playing', paused: 'Paused', completed: 'Complete', interrupted: 'Connection interrupted' }[state.mode];
  $('replay-state').textContent = modeText;
  $('replay-day').textContent = `Day ${state.view + 1} / ${model?.totalDays || 0}`;
  $('replay-date').textContent = model?.frames[state.view]?.date || 'Before first trading day';
  $('replay-progress').max = model?.totalDays || 1; $('replay-progress').value = state.view + 1;
  $('replay-play').disabled = !model || (model.complete && state.view === model.frames.length - 1);
  $('replay-play').textContent = playing ? 'Pause' : state.mode === 'interrupted' ? 'Resume' : 'Play';
  $('replay-restart').disabled = !model || !model.frames.length;
  $('replay-previous').disabled = !model || state.view < 0;
  $('replay-next').disabled = !model || (model.complete && state.view === model.frames.length - 1);
  $('replay-speed').disabled = !model; $('replay-speed').value = String(state.speed);
  $('replay-seek').max = Math.max(0, (model?.frames.length || 0) - 1);
  $('replay-seek').value = Math.max(0, state.view);
  $('replay-seek').disabled = !model || model.frames.length < 2;
  $('replay-error').hidden = !state.error;
  $('replay-error').textContent = state.error?.status === 404 ? 'This run is no longer stored on the server. Received days remain available; a new backtest is needed to fetch the remaining history.' : state.error?.message || '';
  if (model && state.mode !== lastReplayMode) log(`Historical replay · ${modeText.toLowerCase()}`);
  lastReplayMode = state.mode;
  renderCharts({ animate: playing, force: !playing && state.mode !== 'completed' });
  renderDetails();
}

function tableRows(id, values, columns, empty) {
  const body = $(id); body.replaceChildren();
  if (!values.length) {
    const row = document.createElement('tr'), cell = document.createElement('td');
    cell.colSpan = columns; cell.className = 'table-empty'; cell.textContent = empty; row.append(cell); body.append(row); return;
  }
  for (const valuesOfRow of values) {
    const row = document.createElement('tr');
    valuesOfRow.forEach((value, index) => {
      const cell = document.createElement(index === 0 ? 'th' : 'td');
      if (index === 0) cell.scope = 'row';
      cell.textContent = value ?? '—'; row.append(cell);
    });
    body.append(row);
  }
}
function renderPages() {
  const orders = pageRows(detailState?.orders || [], orderPage), events = pageRows(detailState?.events || [], eventPage);
  orderPage = orders.page; eventPage = events.page;
  $('order-count').textContent = `${orders.total} ORDERS`; $('event-count').textContent = `${events.total} EVENTS`;
  for (const [name, page] of [['orders', orders], ['events', events]]) {
    $(`${name}-page`).textContent = `Page ${page.page + 1} / ${page.pages}`;
    $(`${name}-prev`).disabled = page.page === 0; $(`${name}-next`).disabled = page.page === page.pages - 1;
  }
  tableRows('order-rows', orders.rows.map(o => [
    `${o.symbol} · ${o.id}`, o.action.toUpperCase(), quantity(o.quantity), o.status,
    money(o.filled_price), `${orderDate(o.created_at)} / ${o.filled_on || '—'}`,
  ]), 6, 'No orders observed at this replay day.');
  tableRows('event-rows', events.rows.map(e => [
    e.date, e.type, e.symbol, e.action.toUpperCase(), money(e.price), quantity(e.quantity),
  ]), 6, 'Historical events appear during replay.');
}
function renderDetails() {
  if (detailModel === replay.model && detailView === replay.view) return;
  const changedModel = detailModel !== replay.model;
  if (changedModel) stockPage = 0;
  detailModel = replay.model; detailView = replay.view;
  detailState = detailsAt(detailModel, detailView); orderPage = 0; eventPage = 0;
  $('details-date').textContent = detailState.date ? `${detailState.date} · Daily close` : 'Awaiting replay';
  const account = detailState.account;
  $('day-cash').textContent = money(account?.cash); $('day-buying-power').textContent = money(account?.buying_power);
  $('day-open-positions').textContent = account?.open_positions ?? '—';
  $('day-return').textContent = account ? `${money(account.gain)} (${percent(account.return_percent)})` : '—';
  $('day-return').classList.toggle('positive', !!account && account.gain > 0);
  $('day-return').classList.toggle('negative', !!account && account.gain < 0);
  tableRows('position-rows', detailState.positions.map(p => [
    p.symbol, quantity(p.quantity), money(p.entry_price), money(p.current_price), money(p.market_value), money(p.unrealized_pnl),
    `${percent(p.target_percent)} / ${percent(p.actual_percent)}`, `${money(p.stop_loss_price)} / ${money(p.take_profit_price)}`,
  ]), 8, 'Positions appear after the first replay day.');
  renderPages(); renderPortfolio();
  if (changedModel) {
    $('configuration-values').replaceChildren();
    for (const [label, value] of runConfiguration(detailModel?.result)) {
      const row = document.createElement('div'), term = document.createElement('dt'), description = document.createElement('dd');
      term.textContent = label; description.textContent = value ?? 'Unavailable'; row.append(term, description); $('configuration-values').append(row);
    }
    if (!detailModel) { const message = document.createElement('p'); message.textContent = 'Awaiting a completed backtest.'; $('configuration-values').append(message); }
  }
}

function applyPreset() {
  if (!limits || $('period-preset').value === 'custom') return;
  const days = Math.min(Number($('period-preset').value), limits.max_range_days);
  $('end-date').value = limits.latest_end_date;
  const start = shiftDate(limits.latest_end_date, -(days - 1));
  $('start-date').value = start < limits.earliest_start_date ? limits.earliest_start_date : start;
}
function checkLimits(value) {
  if (!value || !['max_range_days', 'max_snapshots', 'max_symbols'].every(key => Number.isInteger(value[key]) && value[key] > 0) || value.max_range_days < 2 || !Number.isFinite(value.percent_total_tolerance) || value.percent_total_tolerance < 0 || !Number.isFinite(dateValue(value.latest_end_date)) || !Number.isFinite(dateValue(value.earliest_start_date)) || value.latest_end_date <= value.earliest_start_date) throw new Error('The server returned invalid form limits.');
  return value;
}
async function connect() {
  $('config-retry').hidden = true;
  try {
    limits = checkLimits(await api.config());
    for (const option of $('period-preset').options) option.disabled = option.value !== 'custom' && Number(option.value) > limits.max_range_days;
    if (!settings) {
      $('period-preset').value = limits.max_range_days >= 90 ? '90' : limits.max_range_days >= 30 ? '30' : 'custom';
      if ($('period-preset').value === 'custom') {
        $('end-date').value = limits.latest_end_date;
        $('start-date').value = shiftDate(limits.latest_end_date, -(limits.max_range_days - 1));
      } else applyPreset();
      settings = { ...readSettings(), preset: $('period-preset').value };
    }
    for (const id of ['start-date', 'end-date']) { $(id).min = limits.earliest_start_date; $(id).max = limits.latest_end_date; }
    $('date-limit-note').textContent = `Available from ${limits.earliest_start_date}, subject to each stock’s history. Up to ${limits.max_range_days} calendar days per run. Dates use ${limits.market_timezone}.`;
    $('portfolio-fields').disabled = false; $('settings-fields').disabled = false;
    $('settings-save').disabled = false; $('settings-open').disabled = false; $('settings-edit').disabled = false;
    setSummary(); updateTotal(); renderState({ phase: 'idle' });
  } catch (error) {
    $('progress-title').textContent = 'Local server unavailable'; $('status-detail').textContent = error.message;
    $('top-status').textContent = '● Not connected'; $('config-retry').hidden = false;
  }
}

$('settings-open').addEventListener('click', showSettings); $('settings-edit').addEventListener('click', showSettings);
$('settings-close').addEventListener('click', () => dialog.close()); $('settings-cancel').addEventListener('click', () => dialog.close());
dialog.addEventListener('close', () => { if (allocationDraft) { $('allocation-rows').replaceChildren(); allocationDraft.forEach(row => addRow(row.symbol, row.percent)); allocationDraft = null; } });
$('period-preset').addEventListener('change', applyPreset);
for (const id of ['start-date', 'end-date']) $(id).addEventListener('input', () => $('period-preset').value = 'custom');
$('settings-form').addEventListener('submit', event => {
  event.preventDefault(); if (!limits || busy) return;
  const proposed = readSettings();
  const { errors } = validateForm({ ...proposed, allocations: rows() }, limits);
  ['capital-error', 'start-error', 'end-error'].forEach(id => $(id).textContent = '');
  showErrors(errors);
  if (Object.keys(errors).length) { inputFor(Object.keys(errors)[0])?.focus(); return; }
  settings = { ...proposed, preset: $('period-preset').value }; setSummary(); allocationDraft = null; dialog.close();
});
$('add-stock').addEventListener('click', () => { if (!busy && limits && rows().length < limits.max_symbols) { addRow(); $('allocation-rows').lastChild.querySelector('input').focus(); } });
$('backtest-form').addEventListener('submit', async event => {
  event.preventDefault(); if (!limits || busy) return;
  const proposed = readSettings();
  const { errors, request } = validateForm({ ...proposed, allocations: rows() }, limits);
  showErrors(errors);
  if (Object.keys(errors).length) {
    const field = Object.keys(errors)[0];
    if (['initial_capital', 'start_date', 'end_date'].includes(field)) { dialog.showModal(); }
    (inputFor(field) || $('allocation-rows').querySelector('input'))?.focus();
    return;
  }
  settings = { ...proposed, preset: $('period-preset').value }; setSummary(); allocationDraft = null; dialog.close();
  await controller.submit(request);
});
$('retry-button').addEventListener('click', () => controller.resume());
$('config-retry').addEventListener('click', connect);
$('chart-symbol').addEventListener('change', () => { chartSymbol = $('chart-symbol').value; renderCharts(); renderPortfolio(); });
$('replay-play').addEventListener('click', () => { if (replay.mode === 'playing') replay.pause(); else void replay.play(); });
$('replay-restart').addEventListener('click', () => void replay.restart());
$('replay-previous').addEventListener('click', () => replay.previous());
$('replay-next').addEventListener('click', () => void replay.next());
$('replay-speed').addEventListener('change', () => void replay.setSpeed(Number($('replay-speed').value)));
$('replay-seek').addEventListener('input', () => replay.seek(Number($('replay-seek').value)));
for (const name of ['orders', 'events']) {
  for (const [direction, delta] of [['prev', -1], ['next', 1]]) {
    $(`${name}-${direction}`).addEventListener('click', () => { if (name === 'orders') orderPage += delta; else eventPage += delta; renderPages(); });
  }
}
$('replay-smooth').addEventListener('change', () => renderCharts({ force: true }));
reducedMotion.addEventListener('change', () => renderCharts({ force: true }));
new ResizeObserver(() => renderCharts({ force: true })).observe($('price-chart-wrap'));
new ResizeObserver(() => renderCharts({ force: true })).observe($('equity-chart-wrap'));
window.addEventListener('pagehide', () => { controller.stop(); replay.pause(false); resetChart($('price-chart')); resetChart($('equity-chart')); });


function signedMoney(value) { return `${value > 0 ? '+' : ''}${money(value)}`; }
function signedPercent(value) { return `${value > 0 ? '+' : ''}${percent(value)}`; }
function tone(element, value) { element.classList.toggle('positive', value > 0); element.classList.toggle('negative', value < 0); }
function renderPortfolio() {
  const account = detailState?.account;
  $('portfolio-date').textContent = detailState?.date ? `${detailState.date} · Daily close` : 'Awaiting replay';
  $('portfolio-equity').textContent = money(account?.equity);
  $('portfolio-return').textContent = account ? `${signedMoney(account.gain)} (${signedPercent(account.return_percent)})` : 'Return · —';
  tone($('portfolio-return'), account?.gain);
  $('portfolio-cash').textContent = money(account?.cash); $('portfolio-buying-power').textContent = money(account?.buying_power); $('portfolio-open').textContent = account?.open_positions ?? '—';
  const page = pageRows(detailState?.positions || [], stockPage, 4); stockPage = page.page;
  $('stocks-page').textContent = `Page ${page.page + 1} / ${page.pages}`;
  $('stocks-prev').disabled = page.page === 0; $('stocks-next').disabled = page.page === page.pages - 1;
  const cards = $('stock-cards'); cards.replaceChildren();
  if (!page.rows.length) { const note = document.createElement('p'); note.className = 'quiet-note'; note.textContent = 'Your stocks appear after the first replay day.'; cards.append(note); }
  for (const position of page.rows) {
    const card = document.createElement('button'); card.type = 'button'; card.className = 'stock-card'; card.setAttribute('aria-pressed', String(chartSymbol === position.symbol)); card.setAttribute('aria-label', `Show ${position.symbol} price chart`);
    const head = document.createElement('div'); head.className = 'stock-card-head';
    const symbol = document.createElement('strong'), allocation = document.createElement('span'); symbol.textContent = position.symbol; allocation.textContent = `${percent(position.target_percent)} target`; head.append(symbol, allocation);
    const value = document.createElement('strong'); value.className = 'stock-card-value'; value.textContent = money(position.market_value);
    const meta = document.createElement('div'); meta.className = 'stock-card-meta';
    const price = document.createElement('span'), pnl = document.createElement('span'); price.textContent = `${money(position.current_price)} close`; pnl.textContent = position.unrealized_pnl === null ? 'No open holding' : `${signedMoney(position.unrealized_pnl)} unrealized`; tone(pnl, position.unrealized_pnl); meta.append(price, pnl);
    const values = (replay.model?.frames.slice(0, replay.view + 1) || []).slice(-40).map(frame => frame.snapshots.find(s => s.bar.symbol === position.symbol).bar.close);
    const ns = 'http://www.w3.org/2000/svg', spark = document.createElementNS(ns, 'svg'), line = document.createElementNS(ns, 'polyline'); spark.setAttribute('viewBox', '0 0 160 35'); spark.setAttribute('aria-hidden','true'); spark.classList.add('stock-spark');
    const lo = Math.min(...values), range = Math.max(...values) - lo || 1;
    line.setAttribute('points', values.map((v,i) => `${values.length === 1 ? 80 : i / (values.length - 1) * 160},${30 - (v-lo)/range*25}`).join(' ')); spark.append(line);
    card.append(head, value, meta, spark); cards.append(card);
    card.addEventListener('click', () => { chartSymbol = position.symbol; $('chart-symbol').value = chartSymbol; renderCharts(); renderPortfolio(); });
  }
}
for (const [name, delta] of [['prev', -1], ['next', 1]]) $('stocks-'+name).addEventListener('click', () => { stockPage += delta; renderPortfolio(); });

addRow('AAPL', 60); addRow('MSFT', 40);
await connect();
