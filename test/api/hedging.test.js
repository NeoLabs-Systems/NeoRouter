'use strict';

require('../support/env');
process.env.NEOROUTER_ROUTING_HEDGE_UNMEASURED_MS = '200';
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { createMockProvider } = require('../support/mock_provider');

let app;
let mock;
let session;
let key;

test.before(async () => {
  app = createApp();
  mock = await createMockProvider({ models: ['hang-one', 'ok-two'] });
  session = (await request(app).post('/api/v1/auth/register').send({ username: 'hedge', email: 'h@example.test', password: 'correct-horse-battery' })).body.session.token;
  await request(app).post('/api/v1/providers/accounts').set('Authorization', `Bearer ${session}`)
    .send({ definitionId: 'custom_openai', slug: 'mock', baseUrl: mock.url, secret: 'good-key', billingMode: 'free_tier' });
  key = (await request(app).post('/api/v1/keys').set('Authorization', `Bearer ${session}`).send({ name: 'k', mode: 'free_only' })).body.secret;
});

test.after(() => mock.close());

test('a hanging free model is hedged and the faster answer wins', async () => {
  const started = Date.now();
  const responses = await Promise.all(Array.from({ length: 6 }, () => request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${key}`)
    .send({ model: 'auto', messages: [{ role: 'user', content: 'hi' }] })));
  for (const response of responses) {
    assert.equal(response.status, 200, JSON.stringify(response.body));
    assert.equal(response.headers['x-neorouter-model'], 'mock/ok-two');
  }
  assert.ok(Date.now() - started < 4500, 'no request waited for the hanging model');
  const logs = (await request(app).get('/api/v1/usage/logs').set('Authorization', `Bearer ${session}`)).body.logs;
  assert.ok(logs.some((log) => log.trail.some((step) => step.outcome === 'hedge_lost')), 'at least one request hedged');
});

test('explicit models only hedge to the same model on other providers', async () => {
  const response = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${key}`)
    .send({ model: 'mock/hang-one', messages: [{ role: 'user', content: 'hi' }] });
  assert.equal(response.status, 200);
  assert.equal(response.headers['x-neorouter-model'], 'mock/hang-one');
});
