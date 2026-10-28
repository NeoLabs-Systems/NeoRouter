'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { createMockProvider } = require('../support/mock_provider');

let app;
let mock;
let session;
const api = (method, path) => request(app)[method](`/api/v1${path}`).set('Authorization', `Bearer ${session}`);

test.before(async () => {
  app = createApp();
  mock = await createMockProvider({ models: ['ok-listed'] });
  session = (await request(app).post('/api/v1/auth/register').send({ username: 'custom', email: 'c@example.test', password: 'correct-horse-battery' })).body.session.token;
});

test.after(() => mock.close());

test('probe lists models without saving the account', async () => {
  const response = await api('post', '/providers/probe').send({ definitionId: 'custom_openai', baseUrl: mock.url, secret: 'good-key' });
  assert.equal(response.status, 200);
  assert.equal(response.body.validation.status, 'healthy');
  assert.deepEqual(response.body.models.map((model) => model.id), ['ok-listed']);
  assert.equal((await api('get', '/providers/accounts')).body.accounts.length, 0);
});

test('manual models are added and used when /models fails', async () => {
  const merged = await api('post', '/providers/probe').send({ definitionId: 'custom_openai', baseUrl: mock.url, secret: 'good-key', options: { manualModels: ['ok-extra'] } });
  assert.deepEqual(merged.body.models.map((model) => model.id).sort(), ['ok-extra', 'ok-listed']);
  const created = await api('post', '/providers/accounts').send({ definitionId: 'custom_openai', slug: 'nolist', baseUrl: `${mock.url}/missing`, secret: 'k', options: { manualModels: ['ok-manual'] } });
  assert.equal(created.status, 201, JSON.stringify(created.body));
  assert.equal(created.body.sync.models, 1);
});

test('OpenRouter connect only redirects back into this instance', async () => {
  const bad = await api('post', '/oauth/openrouter/start').send({ returnTo: 'https://evil.example/' });
  assert.equal(bad.status, 400);
  const good = await api('post', '/oauth/openrouter/start').send({ returnTo: 'http://127.0.0.1:5699/' });
  assert.equal(good.status, 200);
  const url = new URL(good.body.url);
  assert.equal(url.origin, 'https://openrouter.ai');
  assert.equal(url.searchParams.get('code_challenge_method'), 'S256');
  assert.match(url.searchParams.get('callback_url'), /\/api\/v1\/oauth\/openrouter\/callback\//);
  const expired = await request(app).get('/api/v1/oauth/openrouter/callback/unknown-state?code=x');
  assert.equal(expired.status, 400);
});
