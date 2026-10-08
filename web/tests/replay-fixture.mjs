export function fixture() {
  const dates = ['2026-01-05', '2026-01-06', '2026-01-07'];
  const accounts = [{ cash: 1000, buying_power: 1000, equity: 1000 }, { cash: 899, buying_power: 899, equity: 1001 }, { cash: 899, buying_power: 899, equity: 998 }];
  const snapshots = dates.flatMap((date, day) => ['AAPL', 'MSFT'].map((symbol, stock) => {
    const close = stock ? 200 + day : [100, 102, 99][day];
    const open = stock ? close : [100, 101, 100][day];
    const timestamp = `${date}T${stock ? '12' : '14'}:00:00Z`;
    return {
      timestamp, bar: { symbol, timestamp, open, close, high: Math.max(open, close) + 1, low: Math.min(open, close) - 1 },
      indicator: { vwap: day ? close : null, upper_band: day ? close + 2 : null, lower_band: day ? close - 2 : null, standard_deviation: stock ? 0 : null },
      account: accounts[day], position: { symbol, quantity: stock || !day ? 0 : 1, current_price: close, entry_price: stock || !day ? null : 101, stop_loss_price: stock || !day ? null : 95, take_profit_price: stock || !day ? null : 110 },
      signal: { symbol, action: day ? 'hold' : 'buy' },
      submitted_order: !stock && !day ? { id: 'buy-1', symbol, created_at: '2026-01-05T14:00:00Z', action: 'buy', status: 'submitted', filled_price: null, quantity: 1 } : null,
      filled_order: !stock && day === 1 ? { id: 'buy-1', symbol, created_at: '2026-01-05T14:00:00Z', action: 'buy', status: 'filled', filled_price: 101, quantity: 1 } : null,
    };
  }));
  const result = { id: 'fixture', request: { initial_capital: 1000, allocations: [{ symbol: 'AAPL', percent: 60 }, { symbol: 'MSFT', percent: 40 }], start_date: dates[0], end_date: dates[2] }, settings: { volatility_window: 20 }, data: { timeframe: '1Day' }, snapshots, final_account: accounts[2], final_positions: { AAPL: snapshots[4].position, MSFT: snapshots[5].position } };
  const event = (id, type, data) => ({ id: String(id), type, data: JSON.stringify(data) });
  const portfolio = day => ({ day_index: day, date: dates[day], account: accounts[day], positions: { AAPL: snapshots[day * 2].position, MSFT: snapshots[day * 2 + 1].position } });
  // Explicit expected wire sequence, independent of the client timeline model.
  const events = [
    event(1, 'start', { job_id: 'fixture', total_days: 3, total_snapshots: 6, request: result.request, settings: result.settings, data: result.data }),
    event(2, 'snapshot', { day_index: 0, snapshot_index: 0, snapshot: snapshots[0] }),
    event(3, 'snapshot', { day_index: 0, snapshot_index: 1, snapshot: snapshots[1] }),
    event(4, 'portfolio', portfolio(0)),
    event(5, 'snapshot', { day_index: 1, snapshot_index: 2, snapshot: snapshots[2] }),
    event(6, 'snapshot', { day_index: 1, snapshot_index: 3, snapshot: snapshots[3] }),
    event(7, 'portfolio', portfolio(1)),
    event(8, 'snapshot', { day_index: 2, snapshot_index: 4, snapshot: snapshots[4] }),
    event(9, 'snapshot', { day_index: 2, snapshot_index: 5, snapshot: snapshots[5] }),
    event(10, 'portfolio', portfolio(2)),
    event(11, 'complete', { job_id: 'fixture', total_days: 3, total_snapshots: 6, final_account: result.final_account, final_positions: result.final_positions }),
  ];
  return { result, events };
}
