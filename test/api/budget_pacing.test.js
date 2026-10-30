'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { createMockProvider } = require('../support/mock_provider');
const { getDatabase } = require('../../server/db/database');
const pacing = require('../../server/services/budget/pacing');

let app;
let mock;
let session;
const keys = {};

const api = (method, path) => request(app)[method](`/api/v1${path}`).set('Authorization', `Bearer ${session}`);

test.before(async () => {
  // Mid-month, so "gone long before the month ends" holds on any real date
  // (near the 31st, even a heavy spend has no time left to overshoot).
  const today = new Date();
  test.mock.timers.enable({ apis: ['Date'], now: Date.UTC(today.getUTCFullYear(), today.getUTCMonth(), 10, 12) });
  app = createApp();
  mock = await createMockProvider({
    models: ['ok-cheap', 'ok-pricey'],
    prices: { 'ok-cheap': { prompt: '0.0000001', completion: '0.0000002' }, 'ok-pricey': { prompt: '0.00003', completion: '0.00006' } },
  });
  session = (await request(app).post('/api/v1/auth/register').send({ username: 'pacer', email: 'pacer@example.test', password: 'correct-horse-battery' })).body.session.token;
  assert.equal((await api('post', '/providers/accounts').send({ definitionId: 'custom_openai', slug: 'paid', baseUrl: mock.url, secret: 'good-key', billingMode: 'paid' })).status, 201);
  for (const name of ['fresh', 'spent']) {
    const created = await api('post', '/keys').send({ name, mode: 'paid', budgetUsd: 100, budgetPeriod: 'monthly' });
    assert.equal(created.status, 201, JSON.stringify(created.body));
    keys[name] = { id: created.body.key.id, secret: created.body.secret };
  }
  // The "spent" key burned 60 of its 100 USD in the last day: at that rate
  // the budget is gone long before the month ends.
  getDatabase().prepare('INSERT INTO budget_ledger (router_key_id, request_id, amount_usd, created_at) VALUES (?, ?, ?, ?)').run(keys.spent.id, null, 60, Date.now() - 20 * 3_600_000);
  pacing.resetForTests();
});

test.after(() => {
  test.mock.timers.reset();
  return mock.close();
});

async function picks(name, count = 20) {
  const counts = {};
  for (let index = 0; index < count; index += 1) {
    const response = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${keys[name].secret}`)
      .send({ model: 'auto', messages: [{ role: 'user', content: 'hi' }] });
    assert.equal(response.status, 200, JSON.stringify(response.body));
    counts.pace = response.headers['x-neorouter-budget-pace'];
    counts[response.headers['x-neorouter-model']] = (counts[response.headers['x-neorouter-model']] || 0) + 1;
  }
  return counts;
}

test('a budget running out before month end steers routing to cheaper models', async () => {
  const fresh = await picks('fresh');
  const spent = await picks('spent');
  assert.equal(spent.pace, 'tight');
  assert.notEqual(fresh.pace, 'tight');
  const cheapShare = (counts) => (counts['paid/ok-cheap'] || 0) / 20;
  assert.ok(cheapShare(spent) >= 0.9, `tight budget picked cheap ${cheapShare(spent) * 20}/20`);
  assert.ok(cheapShare(spent) > cheapShare(fresh) || cheapShare(fresh) === 1, `fresh ${JSON.stringify(fresh)} vs spent ${JSON.stringify(spent)}`);
});

test('keys report how their budget is pacing', async () => {
  const list = (await api('get', '/keys')).body.keys;
  const byName = Object.fromEntries(list.map((key) => [key.name, key]));
  assert.equal(byName.spent.pacing.state, 'tight');
  assert.ok(byName.spent.pacing.forecast > 100);
  assert.ok(byName.spent.periodResetsAt);
});
