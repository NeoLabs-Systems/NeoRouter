'use strict';
require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { createMockProvider } = require('../support/mock_provider');
const { getDatabase } = require('../../server/db/database');
const processing = require('../../server/services/processing/processing_service');
const consent = require('../../server/services/benchmarks/consent');
const store = require('../../server/services/benchmarks/private_store');
const transport = require('../../server/services/benchmarks/transport');
const quota = require('../../server/services/quota/quota_service');
const load = require('../../server/services/routing/load_tracker');
const catalog = require('../../server/services/catalog/catalog_service');
const stats = require('../../server/services/routing/stats_store');
let app, upstream, session, other, owner, key, model, slow, account;
const api = (method, path, token = session) => request(app)[method](`/api/v1${path}`).set('Authorization', `Bearer ${token}`);
const db = () => getDatabase();
const staged = () => db().prepare('SELECT * FROM benchmark_samples WHERE user_id = ? ORDER BY created_at').all(owner);
const drain = async () => { await new Promise(setImmediate); await new Promise(setImmediate); };

test.before(async () => {
  app = createApp();
  const registration = await request(app).post('/api/v1/auth/register').send({ username: 'benchmark-owner', email: 'benchmark-owner@example.test', password: 'correct-horse-battery' });
  session = registration.body.session.token; owner = registration.body.user.id;
  await api('patch', '/admin/settings').send({ registrationOpen: true });
  other = (await request(app).post('/api/v1/auth/register').send({ username: 'benchmark-other', email: 'benchmark-other@example.test', password: 'correct-horse-battery' })).body.session.token;
  upstream = await createMockProvider({ models: ['echo-benchmark', 'slow-benchmark'], prices: {
    'echo-benchmark': { prompt: '0', completion: '0' }, 'slow-benchmark': { prompt: '0', completion: '0' },
  } });
  const created = await api('post', '/providers/accounts').send({ definitionId: 'custom_openai', baseUrl: upstream.url, secret: 'good-key', billingMode: 'free_tier' });
  assert.equal(created.status, 201, JSON.stringify(created.body));
  account = created.body.account;
  const models = catalog.listForUser(owner);
  model = models.find((entry) => entry.modelId === 'echo-benchmark');
  slow = models.find((entry) => entry.modelId === 'slow-benchmark');
});
test.after(async () => upstream.close());

test('consent controls default off, require replay permission, and are personal to the authenticated account', async () => {
  assert.equal((await api('get', '/admin/settings')).body.localBenchmarks.enabled, false);
  assert.equal((await api('get', '/benchmarks/settings')).body.enabled, false);
  const denied = await api('post', '/keys').send({ name: 'too-early', benchmarkCollection: true });
  assert.equal(denied.status, 409);
  assert.equal((await api('get', '/keys')).body.keys.length, 0);
  assert.equal((await api('patch', '/admin/settings', other).send({ localBenchmarksEnabled: true })).status, 403);
  assert.equal((await api('patch', '/admin/settings').send({ localBenchmarksEnabled: true })).status, 200);
  assert.equal((await api('put', '/benchmarks/settings').send({ enabled: true, allowReplay: false })).status, 409);
  assert.equal((await api('put', '/benchmarks/settings').send({ enabled: true, allowReplay: true, retentionDays: 7 })).status, 200);
  const created = await api('post', '/keys').send({ name: 'private evaluation', benchmarkCollection: true });
  assert.equal(created.status, 201);
  assert.equal(created.body.key.benchmarkCollection, true);
  key = created.body;
  assert.equal((await api('get', `/benchmarks/settings?userId=${owner}`, other)).body.enabled, false);
});

test('plain and streamed captures stay sanitized before response placeholders are restored', async () => {
  processing.saveSettings(owner, { privacy: { enabled: true } });
  const chat = (content, stream) => request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${key.secret}`).send({
    model: `${account.slug}/echo-benchmark`, messages: [{ role: 'user', content }], max_tokens: 100, stream,
  });
  const plain = await chat('Reply to plain.person@example.test.', false);
  assert.equal(plain.status, 200, JSON.stringify(plain.body));
  assert.match(plain.body.choices[0].message.content, /plain.person@example.test/);
  const stream = await chat('Now reply to stream.person@example.test.', true);
  assert.equal(stream.status, 200);
  const text = stream.text.split('\n').filter((line) => line.startsWith('data: {')).map((line) => JSON.parse(line.slice(6)).choices?.[0]?.delta?.content || '').join('');
  assert.match(text, /stream.person@example.test/);
  await drain();
  assert.equal(staged().length, 2);
  for (const row of staged()) {
    const sample = store.decrypt(owner, row.payload).sample;
    assert.equal(JSON.stringify(sample).includes('person@example.test'), false);
    assert.match(JSON.stringify(sample), /EMAIL/);
  }
  assert.equal((await api('delete', `/benchmarks/collection?userId=${owner}`, other)).status, 204);
  assert.equal(staged().length, 2, 'another account can only delete its own samples');
  const ordinaryAdmin = (await api('get', '/admin/settings')).body;
  assert.equal(JSON.stringify(ordinaryAdmin).includes('EMAIL'), false);
  assert.deepEqual((await api('get', '/discovery/internal-benchmarks')).body.results, [], 'unvalidated samples never appear globally');
});

test('background transport accounts for real quota, yields to traffic and never writes production statistics', async () => {
  const row = staged()[0];
  const canonical = { messages: [{ role: 'user', content: 'Return a brief greeting.' }], max_tokens: 64 };
  const beforeRequests = db().prepare('SELECT COUNT(*) count FROM requests').get().count;
  const beforeStats = stats.snapshot(model.ref);
  const beforeQuota = quota.usageOf(`m:${model.ref}`).rpd;
  const response = await transport.call(row, model.ref, canonical, new AbortController().signal);
  assert.match(response.content, /greeting/);
  assert.equal(db().prepare('SELECT COUNT(*) count FROM requests').get().count, beforeRequests);
  assert.equal(stats.snapshot(model.ref).samples, beforeStats.samples);
  assert.equal(quota.usageOf(`m:${model.ref}`).rpd, beforeQuota + 1);
  // Traffic elsewhere does not hold it back; traffic on the same account does.
  const elsewhere = load.foreground({ accountId: 'another-account' });
  try { assert.match((await transport.call(row, model.ref, canonical, new AbortController().signal)).content, /greeting/); }
  finally { elsewhere(); }
  // Real traffic on the same account only holds it back once the account is full.
  const limits = require('../../server/services/quota/account_limits');
  limits.set(model.accountId, { concurrent: 1 });
  const release = load.lease(model);
  try { await assert.rejects(transport.call(row, model.ref, canonical, new AbortController().signal), (error) => error.code === 'account_busy'); }
  finally { release(); }
  const count = upstream.calls.length;
  const running = transport.call(row, slow.ref, canonical, new AbortController().signal);
  for (let attempt = 0; attempt < 30 && upstream.calls.length === count; attempt++) await new Promise((resolve) => setTimeout(resolve, 5));
  assert.ok(upstream.calls.length > count);
  // The replay holds the account's one slot; a real request takes it back.
  const unblock = load.foreground(slow);
  try { await assert.rejects(running, (error) => error.code === 'traffic_yield'); } finally { unblock(); }
  assert.equal(stats.snapshot(slow.ref).samples, 0);
  assert.equal(db().prepare('SELECT COUNT(*) count FROM requests').get().count, beforeRequests);
  assert.equal(load.hasForeground(), false);
});

test('quota exhaustion, subscriptions and revoked key permissions prevent background calls', async () => {
  const row = staged()[0];
  const canonical = { messages: [{ role: 'user', content: 'Return a greeting.' }], max_tokens: 64 };
  quota.setLimit(`m:${model.ref}`, 'rpm', 1, 'manual');
  const calls = upstream.calls.length;
  await assert.rejects(transport.call(row, model.ref, canonical, new AbortController().signal));
  assert.equal(upstream.calls.length, calls);
  quota.clearLimit(`m:${model.ref}`, 'rpm');
  db().prepare('UPDATE provider_accounts SET prepaid = 1 WHERE id = ?').run(account.id);
  assert.equal(transport.automaticallyFree(model), false);
  assert.equal(transport.automaticallyFree({ ...model, freeReason: 'manual_free' }), false);
  db().prepare('UPDATE provider_accounts SET prepaid = 0 WHERE id = ?').run(account.id);
  assert.equal((await api('patch', `/keys/${key.key.id}`).send({ benchmarkCollection: false })).status, 200);
  assert.equal(staged().length, 0);
  await assert.rejects(transport.call(row, model.ref, canonical, new AbortController().signal), (error) => error.code === 'consent_revoked');
  assert.equal(upstream.calls.length, calls);
});

test('replays get the output room the model is known to need', () => {
  const stats = require('../../server/services/routing/stats_store');
  const entry = { ...model, maxOutput: 20000 };
  assert.equal(transport.outputRoom(entry, { max_tokens: 768 }), 768);
  stats.raiseMinOutputTokens(model.ref, 14000);
  assert.equal(transport.outputRoom(entry, { max_tokens: 768 }), 14000, 'judge prompts get the learned room');
  stats.raiseMinOutputTokens(model.ref, 50000);
  assert.equal(transport.outputRoom(entry, { max_tokens: 768 }), 20000, 'never past the model or the cap');
});

test('replays get time for the room they were given', () => {
  const entry = { ...model };
  assert.equal(transport.replayTimeout(entry, 100), 45000, 'short replies keep the base timeout');
  assert.ok(transport.replayTimeout(entry, 14000) > 45000, 'a long reasoning reply gets more time');
  assert.equal(transport.replayTimeout(entry, 1000000), 300000, 'never past five minutes');
});
