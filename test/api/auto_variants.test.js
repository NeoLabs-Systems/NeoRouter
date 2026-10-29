'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { createMockProvider } = require('../support/mock_provider');

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
    for (let i = 0; i < 4; i += 1) assert.equal((await chat(secret, model)).headers['x-neorouter-model'], 'free/ok-free', model);
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
