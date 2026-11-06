'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { createMockProvider } = require('../support/mock_provider');
const stats = require('../../server/services/routing/stats_store');
const accountHealth = require('../../server/services/routing/account_health');

let app;
let provider;
let session;
let secret;
let account;
let models;

const api = (method, path) => request(app)[method](`/api/v1${path}`).set('Authorization', `Bearer ${session}`);
const chat = (model = 'auto') => request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${secret}`)
  .send({ model, messages: [{ role: 'user', content: 'hi' }] });
const HOURS = 3_600_000;

test.before(async () => {
  app = createApp();
  provider = await createMockProvider({ models: ['ok-one', 'ok-two'] });
  session = (await request(app).post('/api/v1/auth/register').send({ username: 'luck', email: 'luck@example.test', password: 'correct-horse-battery' })).body.session.token;
  const created = await api('post', '/providers/accounts').send({ definitionId: 'custom_openai', baseUrl: provider.url, secret: 'good-key', billingMode: 'free_tier' });
  assert.equal(created.status, 201, JSON.stringify(created.body));
  account = created.body.account;
  secret = (await api('post', '/keys').send({ name: 'luck', mode: 'free_only' })).body.secret;
  models = (await api('get', '/models')).body.models.filter((model) => model.id.includes('ok-'));
  assert.equal(models.length, 2);
});

test.after(async () => provider.close());

const holdBack = (reason, hours = 5) => {
  for (const model of models) stats.recordFailure(model.ref, { cooldownUntil: Date.now() + hours * HOURS, cooldownReason: reason });
};
const release = () => { for (const model of models) stats.clearCooldown(model.ref); accountHealth.resetForTests(); };

test('with every model cooling down for hours, a request still tries one instead of giving up', async () => {
  holdBack('rate_limited');
  const response = await chat();
  assert.equal(response.status, 200, JSON.stringify(response.body));
  const trail = response.headers['x-neorouter-trail'];
  assert.ok(trail, 'an attempt was made');
  release();
});

test('only a few such tries are made per request', async () => {
  for (const model of models) stats.recordFailure(model.ref, { cooldownUntil: Date.now() + 5 * HOURS, cooldownReason: 'not_available' });
  provider.calls.length = 0;
  const response = await chat('ok-one');
  assert.equal(response.status, 200);
  assert.ok(provider.calls.length <= 3);
  release();
});

test('what a person must fix is not tried: payment and key problems still answer "no model"', async () => {
  holdBack('payment_required', 20);
  const response = await chat();
  assert.equal(response.status, 503, JSON.stringify(response.body));
  assert.equal(response.body.error.code, 'NO_MODEL_AVAILABLE');
  release();
});

test('marking a connection as fixed forgets its cooldowns, failures and rest', async () => {
  holdBack('rate_limited');
  for (let index = 0; index < 40; index += 1) accountHealth.onFailure(account.id);
  assert.ok(accountHealth.restingUntil(account.id), 'resting after many failures');
  const response = await api('post', `/providers/accounts/${account.id}/mark-fixed`);
  assert.equal(response.status, 200, JSON.stringify(response.body));
  assert.equal(response.body.fixed.models >= 2, true);
  assert.equal(accountHealth.restingUntil(account.id), null);
  for (const model of models) assert.equal(stats.availability(model.ref).available, true, 'cooldown gone');
  assert.equal(accountHealth.chancesLeft(account.id), accountHealth.CHANCES_AFTER_FIXED);
  release();
});

test('after marking as fixed the next failures are forgiven, then normal rules apply again', () => {
  const learning = require('../../server/services/routing/learning');
  const { ProviderError, KINDS } = require('../../server/providers/provider_error');
  const entry = { ref: models[0].ref, accountId: account.id, definitionId: 'custom_openai', task: null };
  accountHealth.markFixed(account.id);
  const failure = () => learning.onFailure(entry, new ProviderError(KINDS.RATE_LIMIT, 'Rate limit reached', { status: 429 }), {});
  for (let index = 0; index < accountHealth.CHANCES_AFTER_FIXED; index += 1) failure();
  assert.equal(stats.availability(entry.ref).available, true, 'forgiven failures leave no cooldown');
  failure();
  assert.equal(stats.availability(entry.ref).available, false, 'the next one counts');
  assert.equal(stats.snapshot(entry.ref).failures > 0, true);
  release();
});

test('a success ends the new start', () => {
  accountHealth.markFixed(account.id);
  accountHealth.onSuccess(account.id);
  assert.equal(accountHealth.chancesLeft(account.id), 0);
});

test('marking as fixed needs a signed-in person who manages connections', async () => {
  const response = await request(app).post(`/api/v1/providers/accounts/${account.id}/mark-fixed`);
  assert.equal(response.status, 401);
  assert.equal((await api('post', '/providers/accounts/nope/mark-fixed')).status, 404);
});
