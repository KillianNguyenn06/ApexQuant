import { CameraMotion } from './motion.mjs';

const NS = 'http://www.w3.org/2000/svg';
const finite = value => typeof value === 'number' && Number.isFinite(value);
const numeric = new Intl.NumberFormat('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
const calendar = new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric', timeZone: 'UTC' });
const dateLabel = value => calendar.format(new Date(`${value}T00:00:00Z`));
const motions = new WeakMap();

export function resetChart(svg) { svg.onpointermove = null; svg.onpointerleave = null; svg.onkeydown = null; svg.onblur = null; motions.get(svg)?.clear(); svg.replaceChildren(); svg.dataset.motion = 'settled'; }
export function priceData(frames, symbol, limit = 80) {
  return frames.slice(-limit).map(frame => ({ date: frame.date, snapshot: frame.snapshots.find(s => s.bar.symbol === symbol) })).filter(row => row.snapshot);
}
export function filledMarkers(series) {
  return series.flatMap((row, index) => {
    const order = row.snapshot.filled_order;
    return order?.status === 'filled' && ['buy', 'sell'].includes(order.action) && finite(order.filled_price)
      ? [{ index, action: order.action, price: order.filled_price, quantity: order.quantity }] : [];
  });
}
export function valueBounds(values) {
  const available = values.filter(finite);
  if (!available.length) return [0, 1];
  const lo = Math.min(...available), hi = Math.max(...available);
  const pad = Math.max((hi - lo) * .12, Math.max(Math.abs(lo), Math.abs(hi)) * .003, .01);
  return [lo - pad, hi + pad];
}
// Unavailable values break lines; numeric zero remains a real point.
export function linePaths(values, x, y) {
  const paths = []; let path = '';
  values.forEach((value, i) => {
    if (!finite(value)) { if (path) paths.push(path); path = ''; }
    else path += `${path ? ' L' : 'M'}${x(i).toFixed(2)},${y(value).toFixed(2)}`;
  });
  if (path) paths.push(path);
  return paths;
}
function attributes(element, values) {
  for (const [key, value] of Object.entries(values)) element.setAttribute(key, String(value));
}
function node(tag, values = {}, text) {
  const element = document.createElementNS(NS, tag); attributes(element, values);
  if (text !== undefined) element.textContent = text;
  return element;
}
function setup(svg, values, first, last, height) {
  const width = Math.max(300, svg.getBoundingClientRect().width || 800);
  const left = 12, right = width - 76, top = 28, bottom = height - 32;
  const [min, max] = valueBounds(values);
  const target = { first: first === last ? first - .5 : first, last: first === last ? last + .5 : last, min, max };
  const updates = [];
  svg.replaceChildren(); svg.setAttribute('viewBox', `0 0 ${width} ${height}`);
  const defs = node('defs'), clip = node('clipPath', { id: `${svg.id}-clip` });
  clip.append(node('rect', { x: left - 5, y: 0, width: right - left + 10, height: bottom + 1 }));
  defs.append(clip); svg.append(defs);
  for (let i = 0; i < 5; i++) {
    const line = node('line', { x1: left, x2: right, class: 'chart-grid' });
    const label = node('text', { x: right + 10, class: 'chart-axis' });
    svg.append(line, label);
    updates.push(({ y, camera }) => {
      const value = camera.min + (camera.max - camera.min) * i / 4, py = y(value);
      attributes(line, { y1: py, y2: py }); label.setAttribute('y', py + 4); label.textContent = numeric.format(value);
    });
  }
  const plot = node('g', { 'clip-path': `url(#${svg.id}-clip)` }); svg.append(plot);
  const project = camera => ({ camera, x: i => left + (right - left) * (i - camera.first) / (camera.last - camera.first), y: value => bottom - (value - camera.min) / (camera.max - camera.min) * (bottom - top) });
  function finish(duration) {
    if (!motions.has(svg)) motions.set(svg, new CameraMotion());
    motions.get(svg).move(target, camera => {
      const projection = project(camera); updates.forEach(update => update(projection));
      svg.dataset.motion = camera.first === target.first && camera.last === target.last && camera.min === target.min && camera.max === target.max ? 'settled' : 'running';
    }, duration);
  }
  return { updates, finish, plot, defs, left, right, top, bottom, width, height };
}
function dates(svg, series, first, layout) {
  const count = layout.width < 480 ? 3 : 5;
  const indexes = new Set(Array.from({ length: count }, (_, i) => Math.round((series.length - 1) * i / (count - 1))));
  for (const index of indexes) {
    const label = node('text', { y: layout.height - 10, class: 'chart-axis', 'text-anchor': index === 0 ? 'start' : index === series.length - 1 ? 'end' : 'middle' }, dateLabel(series[index].date));
    svg.append(label);
    layout.updates.push(({ x }) => label.setAttribute('x', Math.max(layout.left, Math.min(layout.right, x(first + index)))));
  }
}
function paths(layout, values, first, className) {
  // Split before motion so a transition cannot bridge unavailable indicators.
  let start = 0;
  while (start < values.length) {
    while (start < values.length && !finite(values[start])) start++;
    let end = start; while (end < values.length && finite(values[end])) end++;
    if (end > start) {
      const offset = start, segment = values.slice(start, end), path = node('path', { class: className }); layout.plot.append(path);
      layout.updates.push(({ x, y }) => path.setAttribute('d', linePaths(segment, i => x(first + offset + i), y)[0]));
    }
    start = end + 1;
  }
}

export function drawPrice(svg, frames, symbol, duration = 0) {
  const width = Math.max(300, svg.getBoundingClientRect().width || 800);
  const limit = Math.max(12, Math.min(80, Math.floor((width - 88) / 9)));
  // Retain one outgoing candle during the pan; the plot clips it at the edge.
  const series = priceData(frames, symbol, limit + 1), visible = series.slice(-limit);
  if (!series.length) { resetChart(svg); return; }
  const first = frames.length - series.length, visibleFirst = frames.length - visible.length;
  const values = visible.flatMap(({ snapshot: s }) => [s.bar.low, s.bar.high, s.indicator.vwap, s.indicator.upper_band, s.indicator.lower_band]);
  const layout = setup(svg, values, visibleFirst, frames.length - 1, width < 480 ? 280 : 330);
  const { updates, plot, left, right, top, bottom } = layout;
  series.forEach(({ date, snapshot: s }, i) => {
    const b = s.bar, group = node('g', { class: b.close >= b.open ? 'candle up' : 'candle down' });
    const wick = node('line'), body = node('rect', { rx: 1 });
    group.append(node('title', {}, `${symbol} · ${date} · Open ${numeric.format(b.open)} · High ${numeric.format(b.high)} · Low ${numeric.format(b.low)} · Close ${numeric.format(b.close)}`), wick, body); plot.append(group);
    updates.push(({ x, y, camera }) => {
      const px = x(first + i), open = y(b.open), close = y(b.close);
      const candleWidth = Math.max(2, Math.min(9, (right - left) / (camera.last - camera.first + 1) * .55));
      attributes(wick, { x1: px, x2: px, y1: y(b.high), y2: y(b.low) });
      attributes(body, { x: px - candleWidth / 2, y: Math.min(open, close), width: candleWidth, height: Math.max(1.5, Math.abs(close - open)) });
    });
  });
  for (const [key, className] of [['upper_band', 'band'], ['lower_band', 'band'], ['vwap', 'vwap']]) paths(layout, series.map(row => row.snapshot.indicator[key]), first, `chart-line ${className}`);
  for (const marker of filledMarkers(series)) {
    const buy = marker.action === 'buy', group = node('g', { class: `trade-marker ${buy ? 'buy' : 'sell'}` });
    const point = node('circle', { r: 3 }), box = node('rect', { width: 32, height: 15, rx: 3 }), text = node('text', { 'text-anchor': 'middle' }, buy ? 'BUY' : 'SELL');
    group.append(node('title', {}, `Filled ${marker.action} · ${marker.quantity} shares at ${numeric.format(marker.price)} · ${series[marker.index].date}`), point, box, text); plot.append(group);
    updates.push(({ x, y }) => {
      const px = x(first + marker.index), py = y(marker.price), labelY = Math.max(top + 12, Math.min(bottom - 14, py + (buy ? 25 : -20)));
      const labelX = Math.max(left + 16, Math.min(right - 16, px));
      group.setAttribute('visibility', px < left - 5 || px > right + 5 ? 'hidden' : 'visible');
      attributes(point, { cx: px, cy: py }); attributes(box, { x: labelX - 16, y: labelY - 10 }); attributes(text, { x: labelX, y: labelY });
    });
  }
  const latest = visible.at(-1).snapshot.bar.close, guide = node('line', { x1: left, x2: right, class: 'price-guide' }), dot = node('circle', { r: 3, class: 'price-dot' }); plot.append(guide, dot);
  updates.push(({ x, y }) => { attributes(guide, { y1: y(latest), y2: y(latest) }); attributes(dot, { cx: x(frames.length - 1), cy: y(latest) }); });
  dates(svg, visible, visibleFirst, layout);
  svg.setAttribute('aria-label', `${symbol} daily prices with VWAP, bands and filled trade markers through ${visible.at(-1).date}. Showing ${visible.length} of ${frames.length} received days.`);
  layout.finish(duration); return visible.length;
}

export function drawEquity(svg, frames, duration = 0, onInspect = () => {}) {
  if (!frames.length) { resetChart(svg); return; }
  const values = frames.map(frame => frame.account.equity), layout = setup(svg, values, 0, frames.length - 1, 190);
  const gradient = node('linearGradient', { id: 'equity-fill', x1: '0', x2: '0', y1: '0', y2: '1' });
  gradient.append(node('stop', { offset: '0%', 'stop-color': '#40d9b4', 'stop-opacity': '.22' }), node('stop', { offset: '100%', 'stop-color': '#40d9b4', 'stop-opacity': '.01' })); layout.defs.append(gradient);
  const area = node('path', { fill: 'url(#equity-fill)' }), line = node('path', { class: 'chart-line equity' }), dot = node('circle', { r: 3.5, class: 'equity-dot' }); layout.plot.append(area, line, dot);
  layout.updates.push(({ x, y }) => {
    const path = linePaths(values, x, y)[0]; area.setAttribute('d', `${path} L${x(values.length - 1)},${layout.bottom} L${x(0)},${layout.bottom} Z`); line.setAttribute('d', path);
    attributes(dot, { cx: x(values.length - 1), cy: y(values.at(-1)) });
  });
  const cross = node('line', { class: 'chart-crosshair', y1: layout.top, y2: layout.bottom, visibility: 'hidden' });
  const selected = node('circle', { class: 'chart-hover-dot', r: 4, visibility: 'hidden' });
  svg.append(cross, selected);
  let projection, hoverIndex = null;
  function inspect(index) {
    hoverIndex = index;
    const visible = index !== null && projection;
    cross.setAttribute('visibility', visible ? 'visible' : 'hidden'); selected.setAttribute('visibility', visible ? 'visible' : 'hidden');
    if (visible) { const px = projection.x(index); attributes(cross, { x1: px, x2: px }); attributes(selected, { cx: px, cy: projection.y(values[index]) }); }
    onInspect(index === null ? null : frames[index]);
  }
  layout.updates.push(p => { projection = p; if (hoverIndex !== null) inspect(hoverIndex); });
  svg.onpointermove = event => {
    const rect = svg.getBoundingClientRect(), px = (event.clientX - rect.left) / rect.width * layout.width;
    if (!projection || px < layout.left || px > layout.right) { inspect(null); return; }
    inspect(equityIndex(px, layout.left, layout.right, projection.camera.first, projection.camera.last, frames.length));
  };
  svg.onpointerleave = () => inspect(null);
  svg.setAttribute('tabindex', '0');
  svg.onkeydown = event => {
    if (!['ArrowLeft', 'ArrowRight', 'Home', 'End', 'Escape'].includes(event.key)) return;
    event.preventDefault();
    if (event.key === 'Escape') inspect(null);
    else if (event.key === 'Home') inspect(0);
    else if (event.key === 'End') inspect(frames.length - 1);
    else inspect(Math.max(0, Math.min(frames.length - 1, (hoverIndex ?? frames.length - 1) + (event.key === 'ArrowLeft' ? -1 : 1))));
  };
  svg.onblur = () => inspect(null);
  dates(svg, frames, 0, layout);
  svg.setAttribute('aria-label', `Portfolio equity through ${frames.at(-1).date}: ${numeric.format(values.at(-1))} US dollars. ${frames.length} received trading days.`);
  layout.finish(duration);
}

// Select only an actually displayed day, including while the camera moves.
export function equityIndex(px, left, right, first, last, count) {
  return Math.max(0, Math.min(count - 1, Math.round(first + (px - left) / (right - left) * (last - first))));
}
export function equityReturn(equity, capital) {
  const gain = equity - capital;
  return { gain, percent: capital > 0 ? gain / capital * 100 : null };
}
