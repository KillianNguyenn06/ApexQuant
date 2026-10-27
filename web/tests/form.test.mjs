import test from 'node:test';
import assert from 'node:assert/strict';
import { dateValue, shiftDate, validateForm } from '../assets/form.mjs';

const limits = { earliest_start_date: '2017-01-01', latest_end_date: '2026-10-06', max_range_days: 366, max_snapshots: 2928, max_symbols: 8, percent_total_tolerance: .0001 };
const input = () => ({ initial_capital: '50000', allocations: [{ symbol: ' aapl ', percent: '60' }, { symbol: 'MSFT', percent: '40' }], start_date: '2025-10-07', end_date: '2026-10-06' });

test('normalizes a valid request without mutating input or including model settings', () => {
  const raw = input(), { request, errors, total } = validateForm(raw, limits);
  assert.deepEqual(errors, {});
  assert.equal(total, 100);
  assert.deepEqual(request.allocations, [{ symbol: 'AAPL', percent: 60 }, { symbol: 'MSFT', percent: 40 }]);
  assert.equal(raw.allocations[0].symbol, ' aapl ');
  assert.deepEqual(Object.keys(request).sort(), ['allocations', 'end_date', 'initial_capital', 'start_date']);
});
test('rejects duplicate symbols, invalid capital and allocations', () => {
  const raw = input(); raw.initial_capital = 'Infinity'; raw.allocations[1] = { symbol: 'aapl', percent: -1 };
  const { errors } = validateForm(raw, limits);
  assert.ok(errors.initial_capital);
  assert.ok(errors['allocations[1].symbol']);
  assert.ok(errors['allocations[1].percent']);
  assert.ok(errors.allocations);
});
test('enforces provider start, completed dates, and actual server workload limits', () => {
  for (const [change, field] of [
    [{ start_date: '2016-12-31' }, 'start_date'],
    [{ start_date: '2026-02-30' }, 'start_date'],
    [{ end_date: '2026-10-07' }, 'end_date'],
    [{ end_date: '2025-10-07' }, 'end_date'],
    [{ start_date: '2024-10-07' }, 'end_date'],
  ]) assert.ok(validateForm({ ...input(), ...change }, limits).errors[field]);
  assert.ok(validateForm(input(), { ...limits, max_snapshots: 100 }).errors.allocations);
  assert.ok(validateForm(input(), { ...limits, max_symbols: 1 }).errors.allocations);
});
test('date arithmetic handles leap days and daylight-saving dates as calendar days', () => {
  assert.equal(shiftDate('2024-03-01', -1), '2024-02-29');
  assert.equal(shiftDate('2026-03-09', -1), '2026-03-08');
  assert.ok(Number.isNaN(dateValue('2025-02-29')));
});
test('accepts 2021 through 2026 with expanded server limits and still respects smaller overrides', () => {
  const raw = {...input(),start_date:'2021-01-01',end_date:'2026-10-06'};
  const expanded = {...limits,max_range_days:3660,max_snapshots:29280};
  assert.deepEqual(validateForm(raw,expanded).errors,{});
  assert.ok(validateForm(raw,limits).errors.end_date);
  const laterLimits = {...expanded,latest_end_date:'2028-01-01'};
  const boundary = {...raw,end_date:'2028-01-01',start_date:shiftDate('2028-01-01',-3659)};
  assert.deepEqual(validateForm(boundary,laterLimits).errors,{});
  assert.ok(validateForm({...boundary,start_date:shiftDate(boundary.start_date,-1)},laterLimits).errors.end_date);
});

test('ten-year minute and daily choices preserve execution mode and respect overrides', () => {
  const expanded = {...limits, earliest_start_date:'2017-01-01',latest_end_date:'2028-01-01',max_range_days:3660,max_snapshots:8*3660*390};
  const allocations=Array.from({length:8},(_,i)=>({symbol:`S${i}`,percent:12.5}));
  for(const timeframe of ['1Min','1Day']) {
    const raw={...input(),timeframe,allocations,start_date:'2017-01-01',end_date:shiftDate('2017-01-01',3659)};
    const checked=validateForm(raw,expanded);
    assert.deepEqual(checked.errors,{});
    assert.equal(checked.request.timeframe,timeframe);
    assert.ok(validateForm({...raw,end_date:shiftDate(raw.end_date,1)},expanded).errors.end_date);
    if(timeframe==='1Min') assert.ok(validateForm(raw,{...expanded,max_snapshots:expanded.max_snapshots-1}).errors.allocations);
  }
});

test('daily rolling VWAP window is validated and excluded from minute requests', () => {
 const raw = {...input(), timeframe:'1Day', vwap_window:'60'};
 const checked = validateForm(raw, limits);
 assert.deepEqual(checked.errors, {}); assert.equal(checked.request.vwap_window,60);
 for (const value of ['',1,253,2.5,'oops']) assert.ok(validateForm({...raw,vwap_window:value},limits).errors.vwap_window);
 assert.equal(validateForm({...raw,timeframe:'1Min'}, {...limits,max_snapshots:1000000}).request.vwap_window,undefined);
});

test('holding deadline and optional reversion settings normalize and validate', () => {
  const base = {initial_capital:10000,allocations:[{symbol:'AAPL',percent:100}],start_date:'2025-01-01',end_date:'2025-01-04',timeframe:'1Day',holding_days:'3',probability_model:'vwap_reversion',reversion_half_life_days:'8'};
  const limits = {max_symbols:8,percent_total_tolerance:.0001,earliest_start_date:'2017-01-01',latest_end_date:'2026-10-08',max_range_days:3660,max_snapshots:29280};
  const validated=validateForm(base,limits);
  assert.deepEqual(validated.errors,{});
  assert.equal(validated.request.holding_days,3);
  assert.equal(validated.request.reversion_half_life_days,8);
  assert.equal(validated.request.probability_model,'vwap_reversion');
  assert.equal(validateForm({...base,probability_model:'gbm'},limits).request.reversion_half_life_days,undefined);
  for (const holding_days of ['',0,1.5,253]) assert.ok(validateForm({...base,holding_days},limits).errors.holding_days);
  for (const reversion_half_life_days of ['',0,-1,253]) assert.ok(validateForm({...base,reversion_half_life_days},limits).errors.reversion_half_life_days);
});

test('auto submits no manual override, off stays plain GBM', () => {
 const raw={...input(),probability_model:'vwap_reversion_auto',reversion_half_life_days:'garbage'};
 const checked=validateForm(raw,limits);
 assert.deepEqual(checked.errors,{});
 assert.equal(checked.request.probability_model,'vwap_reversion_auto');
 assert.equal(checked.request.reversion_half_life_days,undefined);
});
