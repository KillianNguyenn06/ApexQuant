const DAY = 86400000;
const SYMBOL = /^[A-Z][A-Z0-9.-]{0,9}$/;

export function dateValue(value) {
  if (typeof value !== 'string' || !/^\d{4}-\d{2}-\d{2}$/.test(value)) return NaN;
  const parsed = Date.parse(`${value}T00:00:00Z`);
  return Number.isFinite(parsed) && new Date(parsed).toISOString().slice(0, 10) === value ? parsed : NaN;
}

// Calendar arithmetic is independent of browser timezone and daylight-saving changes.
export function shiftDate(value, days) {
  return new Date(dateValue(value) + days * DAY).toISOString().slice(0, 10);
}

export function validateForm(input, limits) {
  const errors = {};
  const capital = Number(input.initial_capital);
  if (!Number.isFinite(capital) || capital <= 0) errors.initial_capital = 'Enter starting capital greater than zero.';
  const allocations = (input.allocations ?? []).map(row => ({ symbol: String(row.symbol ?? '').trim().toUpperCase(), percent: Number(row.percent) }));
  if (allocations.length < 1 || allocations.length > limits.max_symbols) errors.allocations = `Choose 1–${limits.max_symbols} stocks.`;
  const seen = new Set();
  for (const [i, row] of allocations.entries()) {
    if (!SYMBOL.test(row.symbol)) errors[`allocations[${i}].symbol`] = 'Enter a valid stock symbol.';
    else if (seen.has(row.symbol)) errors[`allocations[${i}].symbol`] = 'Each stock can appear only once.';
    seen.add(row.symbol);
    if (!Number.isFinite(row.percent) || row.percent <= 0 || row.percent > 100) errors[`allocations[${i}].percent`] = 'Enter a percentage greater than zero and up to 100.';
  }
  const total = allocations.reduce((sum, row) => sum + row.percent, 0);
  if (!Number.isFinite(total) || Math.abs(total - 100) > limits.percent_total_tolerance) errors.allocations ??= 'Allocations must total 100%.';
  const start = dateValue(input.start_date), end = dateValue(input.end_date);
  if (!Number.isFinite(start)) errors.start_date = 'Choose a valid start date.';
  else if (input.start_date < limits.earliest_start_date) errors.start_date = `Start on or after ${limits.earliest_start_date}.`;
  if (!Number.isFinite(end)) errors.end_date = 'Choose a valid end date.';
  else if (input.end_date > limits.latest_end_date) errors.end_date = `Choose a completed day on or before ${limits.latest_end_date}.`;
  if (Number.isFinite(start) && Number.isFinite(end)) {
    const days = (end - start) / DAY + 1;
    if (days < 2) errors.end_date = 'End date must be after the start date.';
    else if (days > limits.max_range_days) errors.end_date = `This server supports up to ${limits.max_range_days} calendar days per run.`;
    else if (days * allocations.length > limits.max_snapshots) errors.allocations ??= 'Use fewer stocks or a shorter period for this server.';
  }
  return { errors, total, request: { initial_capital: capital, allocations, start_date: input.start_date, end_date: input.end_date } };
}
