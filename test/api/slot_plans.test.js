'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { createMockProvider } = require('../support/mock_provider');
const registry = require('../../server/providers/registry');
const concurrency = require('../../server/services/routing/concurrency');
const slots = require('../../server/services/routing/slot_utilization');
const { getDatabase } = require('../../server/db/database');

let app, mock, token, key, flatId, backupId;
const api = (method, path) => request(app)[method](`/api/v1${path}`).set('Authorization', `Bearer ${token}`);
const chat = () => request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${key}`)
  .send({ model: 'auto', messages: [{ role: 'user', content: 'hi' }] });

test.before(async () => {
  app = createApp();
  mock = await createMockProvider({ models: ['slow-work'], prices: { 'slow-work': { prompt: '0', completion: '0' } } });
  token = (await request(app).post('/api/v1/auth/register').send({ username: 'slots', email: 'slots@example.test', password: 'correct-horse-battery' })).body.session.token;
  const created = await api('post', '/providers/accounts').send({ definitionId: 'custom_openai', slug: 'flat', baseUrl: mock.url, secret: 'good-key', billingMode: 'flat_rate', concurrent: 4, overflowPolicy: 'overflow' });
  assert.equal(created.status, 201, JSON.stringify(created.body));
  flatId = created.body.account.id;
  assert.equal(created.body.account.billingMode, 'flat_rate');
  backupId = (await api('post', '/providers/accounts').send({ definitionId: 'custom_openai', slug: 'backup', baseUrl: mock.url, secret: 'good-key', billingMode: 'free_tier' })).body.account.id;
  key = (await api('post', '/keys').send({ name: 'slots', mode: 'free_only' })).body.secret;
});
test.after(async () => mock.close());

test('four paid slots fill first; two extra requests overflow, and observed utilization is persisted', async () => {
  const results = await Promise.all(Array.from({ length: 6 }, () => chat()));
  assert.ok(results.every((result) => result.status === 200), results.map((result) => result.text).join('\n'));
  assert.equal(results.filter((result) => result.headers['x-neorouter-model'] === 'flat/slow-work').length, 4);
  assert.equal(results.filter((result) => result.headers['x-neorouter-model'] === 'backup/slow-work').length, 2);
  const groups = (await api('get', '/usage/quotas')).body.accounts;
  const flat = groups.find((group) => group.account.id === flatId);
  assert.equal(flat.slots.peakBusy, 4);
  assert.ok(flat.slots.averageBusy > 0 && flat.slots.averageBusy <= 4);
  assert.ok(flat.slots.idleShare >= 0 && flat.slots.idleShare <= 1);
  assert.ok(flat.slots.series.length > 0);
  assert.equal(flat.account.models.free, 0, 'a paid flat-rate subscription is never classified as free');
  assert.ok(getDatabase().prepare('SELECT SUM(busy_ms) busy FROM slot_utilization WHERE account_id = ?').get(flatId).busy > 0);
});

test('queue policy keeps six requests on four slots, and a short maximum wait overflows instead', async () => {
  await api('patch', `/providers/accounts/${flatId}`).send({ overflowPolicy: 'queue', slotWaitMs: 1500 });
  const queued = await Promise.all(Array.from({ length: 6 }, () => chat()));
  assert.ok(queued.every((result) => result.status === 200 && result.headers['x-neorouter-model'] === 'flat/slow-work'));
  await api('patch', `/providers/accounts/${flatId}`).send({ slotWaitMs: 20 });
  const expired = await Promise.all(Array.from({ length: 6 }, () => chat()));
  assert.ok(expired.every((result) => result.status === 200));
  assert.equal(expired.filter((result) => result.headers['x-neorouter-model'] === 'backup/slow-work').length, 2);
  await api('patch', `/providers/accounts/${flatId}`).send({ overflowPolicy: 'auto' });
  const automatic = await Promise.all(Array.from({ length: 6 }, () => chat()));
  assert.equal(automatic.filter((result) => result.headers['x-neorouter-model'] === 'backup/slow-work').length, 2);
  await api('patch', `/providers/accounts/${backupId}`).send({ enabled: false });
  await api('patch', `/providers/accounts/${flatId}`).send({ slotWaitMs: 1500 });
  const noAlternative = await Promise.all(Array.from({ length: 6 }, () => chat()));
  assert.ok(noAlternative.every((result) => result.status === 200 && result.headers['x-neorouter-model'] === 'flat/slow-work'), 'auto queues when no comparable source is free');
});

test('Kourier discovers models dynamically and plan limits yield to account bonuses', () => {
  const definition = registry.get('kourier');
  assert.equal(definition.models.path, '/models');
  assert.equal(definition.models.static, undefined);
  assert.equal(concurrency.limitFor({ accountId: 'kourier-test', definitionId: 'kourier', slotPlan: 'pro' }).limit, 3);
  assert.equal(concurrency.limitFor({ accountId: flatId, definitionId: 'kourier', slotPlan: 'pro' }).limit, 4);
});
