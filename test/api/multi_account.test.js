'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { createMockProvider } = require('../support/mock_provider');
const concurrency = require('../../server/services/routing/concurrency');

let app;
let capped;
let roomy;
let session;
let secret;
let cappedAccount;

const api = (method, path) => request(app)[method](`/api/v1${path}`).set('Authorization', `Bearer ${session}`);
const chat = (model = 'auto') => request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${secret}`)
  .send({ model, messages: [{ role: 'user', content: 'hi' }] });

test.before(async () => {
  app = createApp();
  capped = await createMockProvider({ models: ['slow-shared'], maxConcurrent: 1 });
  roomy = await createMockProvider({ models: ['slow-shared'] });
  session = (await request(app).post('/api/v1/auth/register').send({ username: 'multi', email: 'multi@example.test', password: 'correct-horse-battery' })).body.session.token;
  const add = (url) => api('post', '/providers/accounts').send({ definitionId: 'custom_openai', baseUrl: url, secret: 'good-key', billingMode: 'free_tier' });
  const a = await add(capped.url);
  const b = await add(roomy.url);
  assert.equal(a.status, 201, JSON.stringify(a.body));
  assert.equal(b.status, 201);
  cappedAccount = a.body.account;
  assert.notEqual(a.body.account.slug, b.body.account.slug, 'each account gets its own model-id prefix');
  assert.match(b.body.account.label, / 2$/, 'the second account of a provider is numbered');
  secret = (await api('post', '/keys').send({ name: 'multi', mode: 'free_only' })).body.secret;
});

test.after(async () => {
  await capped.close();
  await roomy.close();
});

test('a concurrency 429 teaches the account its limit and the sibling takes the rest', async () => {
  concurrency.resetForTests();
  // Asking for the capped account by name puts the whole burst on it first.
  const pinned = `${cappedAccount.slug}/slow-shared`;
  const first = await Promise.all([chat(pinned), chat(pinned), chat(pinned)]);
  for (const response of first) assert.equal(response.status, 200, JSON.stringify(response.body));
  const learned = concurrency.limitFor({ accountId: cappedAccount.id, definitionId: 'custom_openai' });
  assert.ok(capped.calls.some((call) => call.rejected), 'the burst overloaded the capped account once');
  assert.deepEqual(learned, { limit: 1, source: 'learned' });

  // With the limit learned, the next burst never overloads that account.
  capped.calls.length = 0;
  roomy.calls.length = 0;
  const second = await Promise.all([chat(pinned), chat(pinned), chat(pinned), chat(pinned)]);
  for (const response of second) assert.equal(response.status, 200, JSON.stringify(response.body));
  assert.equal(capped.calls.filter((call) => call.rejected).length, 0, 'no more 429s from the capped account');
  assert.ok(roomy.calls.length >= 3, `roomy took ${roomy.calls.length} of 4`);
});

test('a concurrency 429 does not cool the model down like a used-up rate limit', async () => {
  const models = (await api('get', '/models')).body.models.filter((model) => model.id.endsWith('/slow-shared'));
  for (const model of models) assert.equal(model.stats.cooldownReason ?? null, null, `${model.id} is not cooling down`);
});

test('sequential traffic for one model spreads over both accounts', async () => {
  concurrency.resetForTests();
  capped.calls.length = 0;
  roomy.calls.length = 0;
  for (let index = 0; index < 12; index += 1) assert.equal((await chat('slow-shared')).status, 200);
  assert.ok(capped.calls.length > 0 && roomy.calls.length > 0, `capped ${capped.calls.length}, roomy ${roomy.calls.length}`);
});

test('learned limits probe upward again and expire', () => {
  concurrency.resetForTests();
  const entry = { accountId: 'acc-x', definitionId: 'custom_openai' };
  assert.equal(concurrency.limitFor(entry), null);
  assert.equal(concurrency.onRateLimited(entry, { message: 'Rate limit reached' }, { running: 1 }), false, 'one request at a time says nothing about concurrency');
  assert.equal(concurrency.limitFor(entry), null);
  concurrency.onRateLimited(entry, { message: 'rate limit' }, { running: 4 });
  assert.equal(concurrency.limitFor(entry).limit, 3);
  for (let index = 0; index < 10; index += 1) concurrency.onSuccess(entry, { running: 3 });
  assert.equal(concurrency.limitFor(entry).limit, 4);
  assert.equal(concurrency.limitFor(entry, Date.now() + 2 * 60 * 60 * 1000), null);
});
