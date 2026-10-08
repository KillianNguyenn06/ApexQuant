const finite = value => typeof value === 'number' && Number.isFinite(value);

// Reconstruct only the received, displayed prefix. Never read future snapshots
// from the completed result when building positions, orders or activity.
export function detailsAt(model, view) {
  const frames = model?.frames.slice(0, Math.max(0, view + 1)) || [];
  const current = frames.at(-1);
  if (!current) return { date: null, account: null, positions: [], orders: [], events: [] };
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
    if (snapshot.filled_order) {
      const order = snapshot.filled_order;
      orders.set(order.id, { ...orders.get(order.id), ...order, observed_at: snapshot.timestamp, filled_on: frame.date });
      events.push({ day, stock, date: frame.date, symbol: snapshot.bar.symbol, type: 'Fill', action: order.action, price: order.filled_price, quantity: order.quantity });
    }
    events.push({ day, stock, date: frame.date, symbol: snapshot.bar.symbol, type: 'Bar', action: snapshot.signal.action, price: snapshot.bar.close, quantity: null });
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
    ['Risk-free rate (%)', finite(d.risk_free_rate) ? d.risk_free_rate * 100 : null], ['Rate observation date', d.risk_free_rate_date ?? 'Unavailable'],
    ['Volatility', `Rolling ${s.volatility_window}-bar window · ${s.periods_per_year} periods/year`],
    ['Fallback volatility (%)', finite(s.initial_volatility) ? s.initial_volatility * 100 : null],
    ['Model horizon (years)', s.time_horizon], ['Simulation paths', s.num_paths], ['Steps per path', s.num_steps],
    ['Seed', s.seed === 0 ? 'Randomized · realized seed not recorded' : s.seed],
  ];
}
