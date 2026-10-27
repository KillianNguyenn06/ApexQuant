const finite = value => typeof value === 'number' && Number.isFinite(value);

// Reconstruct only the received, displayed prefix. Never read future snapshots
// from the completed result when building positions, orders or activity.
export function detailsAt(model, view) {
  const frames = model?.frames.slice(0, Math.max(0, view + 1)) || [];
  const current = frames.at(-1);
  if (!current) return { date: null, account: null, positions: [], orders: [], events: [], tradeAudit: null };
  const positions = model.symbols.map(symbol => {
    const position = current.positions[symbol];
    const marketValue = position.quantity * position.current_price;
    return {
      ...position, market_value: marketValue,
      target_percent: model.result.request.allocations.find(row => row.symbol === symbol).percent,
      actual_percent: current.account.equity > 0 ? marketValue / current.account.equity * 100 : null,
      unrealized_pnl: position.quantity > 0 && finite(position.entry_price) ? (position.current_price - position.entry_price) * position.quantity : null,
    };
  });
  const orders = new Map(), events = [];
  frames.forEach((frame, day) => frame.snapshots.forEach((snapshot, stock) => {
    // Fills execute at this bar's open; new submissions follow its processing.
    for (const order of snapshot.filled_orders ?? (snapshot.filled_order ? [snapshot.filled_order] : [])) {
      orders.set(order.id, { ...orders.get(order.id), ...order, observed_at: snapshot.timestamp, filled_on: frame.date });
      events.push({ day, stock, date: frame.date, symbol: snapshot.bar.symbol, type: 'Fill', action: order.action, price: order.filled_price, quantity: order.quantity });
    }
    events.push({ day, stock, date: frame.date, symbol: snapshot.bar.symbol, type: 'Bar', reason: snapshot.decision_reason, risk: snapshot.risk, action: snapshot.signal.action, price: snapshot.bar.close, quantity: null });
    for (const order of snapshot.order_events ?? []) {
      orders.set(order.id, { ...orders.get(order.id), ...order, observed_at: snapshot.timestamp, filled_on: null });
      events.push({ day, stock, date: frame.date, symbol: snapshot.bar.symbol, type: order.status, action: order.action, price: null, quantity: order.quantity, reason: snapshot.execution_reason });
    }
    if (snapshot.execution_reason && !(snapshot.order_events?.length)) events.push({ day, stock, date: frame.date, symbol: snapshot.bar.symbol, type: 'Execution', action: snapshot.signal.action, price: null, quantity: null, reason: snapshot.execution_reason });
    if (snapshot.submitted_order) {
      const order = snapshot.submitted_order;
      orders.set(order.id, { ...order, observed_at: snapshot.timestamp, filled_on: null });
      events.push({ day, stock, date: frame.date, symbol: snapshot.bar.symbol, type: 'Submitted', action: order.action, price: null, quantity: order.quantity });
    }
  }));
  const gain = current.account.equity - model.result.request.initial_capital;
  return {
    date: current.date,
    account: { ...current.account, open_positions: positions.filter(p => p.quantity > 0).length, gain, return_percent: gain / model.result.request.initial_capital * 100 },
    positions, orders: [...orders.values()].reverse(), events: events.reverse(),
    tradeAudit: {
      opportunities: events.filter(e => e.type === 'Bar' && e.action === 'buy').length,
      submitted: events.filter(e => e.type === 'Submitted' && e.action === 'buy').length,
      buys: events.filter(e => e.type === 'Fill' && e.action === 'buy').length,
      sells: events.filter(e => e.type === 'Fill' && e.action === 'sell').length,
      blocked: events.filter(e => e.type === 'Bar' && e.action === 'buy' && /rejected|not_submitted/.test(e.reason || '')).length,
    },
  };
}

export function pageRows(rows, requested = 0, size = 20) {
  const pages = Math.max(1, Math.ceil(rows.length / size));
  const page = Math.max(0, Math.min(Number.isFinite(requested) ? Math.trunc(requested) : 0, pages - 1));
  return { rows: rows.slice(page * size, (page + 1) * size), page, pages, total: rows.length };
}

// Explicit public metadata allowlist: never dump provider credentials or a
// server configuration object into the page.
export function runConfiguration(result) {
  if (!result) return [];
  const r = result.request, s = result.settings, d = result.data;
  return [
    ['Run ID', result.id], ['Starting capital (USD)', r.initial_capital],
    ['Requested dates', `${r.start_date} → ${r.end_date}`],
    ['Target allocation', r.allocations.map(a => `${a.symbol} ${a.percent}%`).join(' · ')],
    ['Data feed', d.feed], ['Bar frequency', d.timeframe], ['Price adjustment', d.adjustment],
    ['Rate policy', d.rate_policy || 'Legacy constant rate'], ['Initial/fixed risk-free rate (%)', finite(d.risk_free_rate) ? d.risk_free_rate * 100 : null], ['Rate observation date', d.risk_free_rate_date ?? 'Unavailable'],
    ['Volatility', `Rolling ${s.volatility_window}-${d.timeframe === '1Min' ? 'completed daily bars' : 'bar window'} · ${s.periods_per_year} periods/year`],
    ...(d.timeframe === '1Min' ? [['Indicators', 'Session VWAP resets daily; EMA20 uses minute closes and never vetoes a buy'], ['Execution', 'Next-minute entry; protective exits intraminute; ambiguous exits use stop first'], ['Trading window', '09:30–16:00 New York; raw historical bars']] : [['Indicators', s.daily_vwap_window ? `Rolling ${s.daily_vwap_window}-trading-day VWAP/bands; no breakout entry` : 'Legacy cumulative VWAP/bands; no breakout entry'], ['Execution', 'Prior-band touches or lower-band close; next-open entry; protective exits on touch, stop first if ambiguous']]),
    ['Fallback volatility (%)', finite(s.initial_volatility) ? s.initial_volatility * 100 : null],
    ['Maximum holding days', s.holding_days ?? 'Legacy: no deadline'], ['Probability model', ({gbm:'Off · plain GBM',vwap_reversion_auto:'Auto · historical estimate',vwap_reversion:'Manual · chosen half-life'})[s.probability_model] || 'gbm'], ...Object.entries(s.reversion_estimates || {}).flatMap(([symbol, fit]) => [[`${symbol} auto half-life`, `${fit.half_life_days.toFixed(2)} trading days`], [`${symbol} calibration`, `${fit.samples} observations through ${fit.through}; frozen before run`]]), ...(s.probability_model === 'vwap_reversion' ? [['Reversion half-life (trading days)', s.reversion_half_life_days], ['Model assumption', 'Pull toward signal VWAP; manually configured, not calibrated']] : []), ['Model horizon (years)', s.time_horizon], ['Simulation paths', s.num_paths], ['Steps per path', s.num_steps],
    ['Seed', s.seed === 0 ? 'Randomized · realized seed not recorded' : s.seed],
  ];
}
