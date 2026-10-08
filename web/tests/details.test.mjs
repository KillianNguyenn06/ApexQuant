import test from 'node:test';
import assert from 'node:assert/strict';
import { detailsAt, pageRows, runConfiguration } from '../assets/details.mjs';
import { ReplayModel } from '../assets/replay.mjs';
import { fixture } from './replay-fixture.mjs';

function completed() {
  const {result, events} = fixture();
  const model = new ReplayModel(result); events.forEach(e => model.accept(e)); return model;
}
test('details show no future orders or fills, even when all frames have arrived', () => {
  const model = completed();
  const empty = detailsAt(model,-1); assert.equal(empty.account,null); assert.deepEqual(empty.orders,[]);
  const first = detailsAt(model,0);
  assert.equal(first.orders.length,1); assert.equal(first.orders[0].status,'submitted');
  assert.equal(first.orders[0].filled_price,null); assert.equal(first.orders[0].filled_on,null);
  assert.equal(first.events.filter(e=>e.type==='Fill').length,0);
  assert.equal(first.account.open_positions,0); assert.equal(first.positions[0].market_value,0);
  const next = detailsAt(model,1);
  assert.equal(next.orders.length,1); assert.equal(next.orders[0].status,'filled'); assert.equal(next.orders[0].filled_price,101);
  assert.equal(next.orders[0].filled_on,'2026-01-06'); assert.equal(next.orders[0].created_at,'2026-01-05T14:00:00Z');
  assert.equal(next.positions[0].unrealized_pnl,1); assert.equal(next.account.open_positions,1);
});
test('Hold values update; account and position values reconcile independently', () => {
  const model = completed(), state = detailsAt(model,2);
  assert.equal(state.date,'2026-01-07'); assert.equal(state.account.cash,899);
  assert.equal(state.positions[0].market_value,99); assert.equal(state.positions[0].unrealized_pnl,-2);
  assert.equal(state.positions[1].unrealized_pnl,null);
  assert.equal(state.account.cash+state.positions.reduce((sum,p)=>sum+p.market_value,0),998);
  assert.equal(state.account.gain,-2); assert.equal(state.account.return_percent,-.2);
  assert.equal(state.positions[0].target_percent,60); assert.equal(state.positions[0].actual_percent,99/998*100);
  assert.equal(state.events.filter(e=>e.type==='Bar').length,6);
  assert.equal(state.events.filter(e=>e.type==='Fill').length,1);
  assert.equal(state.events.filter(e=>e.type==='Submitted').length,1);
  assert.deepEqual(state.events.slice(0,2).map(e=>[e.date,e.symbol,e.action]),[['2026-01-07','MSFT','hold'],['2026-01-07','AAPL','hold']]);
});
test('rewinding and resetting remove later activity without changing saved source values', () => {
  const model=completed(), original=JSON.stringify(model.result);
  detailsAt(model,2); const back=detailsAt(model,0);
  assert.equal(back.orders[0].status,'submitted'); assert.equal(back.events.length,3);
  assert.equal(JSON.stringify(model.result),original);
  assert.deepEqual(detailsAt(null,0),{date:null,account:null,positions:[],orders:[],events:[]});
});
test('pagination bounds DOM rows and retains every event across pages', () => {
  const values=Array.from({length:47},(_,i)=>i);
  assert.deepEqual([0,1,2].flatMap(p=>pageRows(values,p).rows),values);
  assert.equal(pageRows(values,99).page,2); assert.equal(pageRows(values,-5).page,0);
  assert.deepEqual(pageRows([],0),{rows:[],page:0,pages:1,total:0});
});
test('configuration is an allowlist and does not imply seed-zero reproducibility', () => {
  const {result}=fixture(); result.settings.seed=0; result.settings.initial_volatility=.2; result.data.risk_free_rate=.04;
  result.credentials={api_key:'SECRET'}; result.request.api_secret='SECRET';
  const values=Object.fromEntries(runConfiguration(result));
  assert.equal(values['Seed'],'Randomized · realized seed not recorded');
  assert.equal(values['Fallback volatility (%)'],20); assert.equal(values['Rate observation date'],'Unavailable');
  assert.equal(values['Risk-free rate (%)'],4);
  assert.equal(JSON.stringify(values).includes('SECRET'),false);
  assert.equal(values['Target allocation'],'AAPL 60% · MSFT 40%');
  assert.deepEqual(runConfiguration(null),[]);
});
