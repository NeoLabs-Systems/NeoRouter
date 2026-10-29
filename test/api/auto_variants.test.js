'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { createMockProvider } = require('../support/mock_provider');
const stats = require('../../server/services/routing/stats_store');

let app;
let free;
let paid;
let session;

const api = (method, path) => request(app)[method](`/api/v1${path}`).set('Authorization', `Bearer ${session}`);
const keyFor = async (kinds) => (await api('post', '/keys').send({ name: 'variants', ...kinds })).body.secret;
const ids = async (secret) => (await request(app).get('/v1/models').set('Authorization', `Bearer ${secret}`)).body.data.map((model) => model.id);
const chat = (secret, model) => request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${secret}`).send({ model, messages: [{ role: 'user', content: 'hi' }] });

test.before(async () => {
  app = createApp();
  free = await createMockProvider({ models: ['ok-free'] });
  paid = await createMockProvider({ models: ['ok-paid'], prices: { 'ok-paid': { prompt: '0.000001', completion: '0.000002' } } });
  session = (await request(app).post('/api/v1/auth/register').send({ username: 'variants', email: 'variants@example.test', password: 'correct-horse-battery' })).body.session.token;
  for (const [slug, url, billingMode] of [['free', free.url, 'free_tier'], ['paid', paid.url, 'paid']]) {
    const added = await api('post', '/providers/accounts').send({ definitionId: 'custom_openai', slug, baseUrl: url, secret: 'good-key', billingMode });
    assert.equal(added.status, 201, JSON.stringify(added.body));
  }
});

test.after(async () => {
  await free.close();
  await paid.close();
});

test('a key that may use free and paid models gets auto variants for each kind', async () => {
  const listed = await ids(await keyFor({ allowFree: true, allowPlan: true, allowPaid: true }));
  for (const id of ['auto', 'auto:fast', 'auto-free', 'auto-free:fast', 'auto-paid', 'auto-paid:smart']) assert.ok(listed.includes(id), `${id} in ${listed.join(',')}`);
  assert.ok(!listed.some((id) => id.startsWith('auto-plan')), 'no subscription models, so no subscription variants');
});

test('plain auto follows the key profile and every profile is listed by name', async () => {
  const listed = await ids(await keyFor({ allowFree: true, allowPlan: false, allowPaid: false }));
  assert.deepEqual(listed.filter((id) => id.startsWith('auto')), ['auto', 'auto:balanced', 'auto:fast', 'auto:smart']);
  const secret = await keyFor({ allowFree: true, allowPlan: false, allowPaid: false, profile: 'fast' });
  const models = (await request(app).get('/v1/models').set('Authorization', `Bearer ${secret}`)).body.data;
  assert.equal(models.find((model) => model.id === 'auto').neorouter.profile, 'fast');
  assert.equal((await chat(secret, 'auto:default')).status, 200, 'auto:default is plain auto');
});

test('of two similar models the cheaper one is chosen, whatever the profile', async () => {
  const secret = await keyFor({ allowFree: true, allowPlan: true, allowPaid: true });
  for (const model of ['auto:smart', 'auto:fast', 'auto']) {
    for (let i = 0; i < 4; i += 1) {
      // Equal learned speed and reliability: only the price tells them apart.
      stats.resetForTests();
      assert.equal((await chat(secret, model)).headers['x-neorouter-model'], 'free/ok-free', model);
    }
  }
});

test('an empty choice explains why', async () => {
  const freeOnly = await keyFor({ allowFree: false, allowPlan: true, allowPaid: false });
  const response = await chat(freeOnly, 'auto');
  assert.equal(response.status, 503);
  assert.match(response.body.error.message, /2 are not allowed for this key/);
});

test('a key limited to one kind gets only the plain auto entries', async () => {
  const listed = await ids(await keyFor({ allowFree: true, allowPlan: false, allowPaid: false }));
  assert.ok(listed.includes('auto:fast'));
  assert.ok(!listed.some((id) => id.startsWith('auto-')), listed.join(','));
});

test('a kind variant routes only to that kind of model', async () => {
  const secret = await keyFor({ allowFree: true, allowPlan: true, allowPaid: true });
  for (let i = 0; i < 4; i += 1) {
    assert.equal((await chat(secret, 'auto-paid:fast')).headers['x-neorouter-model'], 'paid/ok-paid');
    assert.equal((await chat(secret, 'auto-free')).headers['x-neorouter-model'], 'free/ok-free');
  }
  const freeOnly = await keyFor({ allowFree: true, allowPlan: false, allowPaid: false });
  assert.notEqual((await chat(freeOnly, 'auto-paid')).status, 200, 'a variant never widens what the key may use');
});

test('a token limit stops free and subscription models until it resets', async () => {
  const created = await api('post', '/keys').send({ name: 'tokens', allowFree: true, allowPlan: false, allowPaid: false, tokenLimit: 20, tokenPeriod: 'daily' });
  assert.equal(created.status, 201, JSON.stringify(created.body));
  assert.equal(created.body.key.tokenLimit, 20);
  const secret = created.body.secret;
  assert.equal((await chat(secret, 'auto')).status, 200, 'the first request fits (the mock reports 18 tokens)');
  assert.equal((await chat(secret, 'auto')).status, 200, '18 of 20 used, still under the limit');
  const blocked = await chat(secret, 'auto');
  assert.equal(blocked.status, 429);
  assert.equal(blocked.body.error.code, 'TOKEN_LIMIT_REACHED');
  const listed = (await api('get', '/keys')).body.keys.find((key) => key.id === created.body.key.id);
  assert.equal(listed.tokensUsed, 36);
  assert.ok(listed.tokenResetsAt);

  // With paid models allowed, the key carries on there.
  await api('patch', `/keys/${created.body.key.id}`).send({ allowPaid: true });
  const paidOn = await chat(secret, 'auto');
  assert.equal(paidOn.status, 200);
  assert.equal(paidOn.headers['x-neorouter-model'], 'paid/ok-paid');
});

test('a custom style chooses kinds of model and how much each counts', async () => {
  const style = await api('post', '/routing/profiles').send({ name: 'Paid first', weights: { intelligence: 0.4, speed: 0.2, reliability: 0.3, cost: 0.1 }, constraints: { kinds: { free: 0, plan: 0, paid: 1 } } });
  assert.equal(style.status, 201, JSON.stringify(style.body));
  const secret = await keyFor({ allowFree: true, allowPlan: true, allowPaid: true });
  for (let i = 0; i < 4; i += 1) assert.equal((await chat(secret, `auto:${style.body.profile.id}`)).headers['x-neorouter-model'], 'paid/ok-paid', 'free models are left out');
  // Own styles appear by their name, only for this account's keys.
  const listed = await ids(secret);
  assert.ok(listed.includes('auto:paid-first'), listed.join(','));
  assert.ok(!listed.some((id) => id.includes('custom_')));
  assert.equal((await chat(secret, 'auto:paid-first')).headers['x-neorouter-model'], 'paid/ok-paid');
  await api('patch', '/admin/settings').send({ registrationOpen: true });
  const other = (await request(app).post('/api/v1/auth/register').send({ username: 'variants2', email: 'variants2@example.test', password: 'correct-horse-battery' })).body.session.token;
  const otherKey = (await request(app).post('/api/v1/keys').set('Authorization', `Bearer ${other}`).send({ name: 'other', mode: 'free_only' })).body.secret;
  assert.ok(!(await ids(otherKey)).includes('auto:paid-first'), 'another account does not see it');
  assert.equal((await chat(otherKey, 'auto:paid-first')).status, 400, 'nor can it use it');
  const leaning = await api('post', '/routing/profiles').send({ name: 'Mostly paid', weights: { intelligence: 0.4, speed: 0.2, reliability: 0.3, cost: 0.1 }, constraints: { kinds: { free: 0.05, plan: 0, paid: 1 } } });
  for (let i = 0; i < 4; i += 1) assert.equal((await chat(secret, `auto:${leaning.body.profile.id}`)).headers['x-neorouter-model'], 'paid/ok-paid', 'a small weight makes free a fallback');
});

test('capacity adds up what the free models could take around the clock', async () => {
  const quota = require('../../server/services/quota/quota_service');
  // The mock's rate-limit headers (1000 requests) are learned as a minute limit:
  // given per minute, not stretched over a day.
  const before = (await api('get', '/usage/capacity')).body;
  assert.equal(before.perDay, 0);
  assert.equal(before.perMinuteOnly, 1000);
  const free = (await api('get', '/models')).body.models.find((model) => model.id === 'free/ok-free');
  quota.setLimit(`m:${free.ref}`, 'rpm', 2, 'learned');
  quota.setLimit(`m:${free.ref}`, 'rpd', 1000, 'learned');
  const after = (await api('get', '/usage/capacity')).body;
  assert.equal(after.perDay, 1000, 'a daily limit counts as it is');
  assert.equal(after.perMinuteOnly, 0);
  assert.equal(after.perHour, 42);
  assert.equal(after.providersCounted, 1);
});
