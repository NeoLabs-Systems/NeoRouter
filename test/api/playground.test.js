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
let keyId;

test.before(async () => {
  app = createApp();
  mock = await createMockProvider({ models: ['ok-two'] });
  session = (await request(app).post('/api/v1/auth/register').send({ username: 'play', email: 'p@example.test', password: 'correct-horse-battery' })).body.session.token;
  const add = (slug, billingMode) => request(app).post('/api/v1/providers/accounts').set('Authorization', `Bearer ${session}`)
    .send({ definitionId: 'custom_openai', slug, baseUrl: mock.url, secret: 'good-key', billingMode });
  await add('freebie', 'free_tier');
  await add('paidmock', 'paid');
  keyId = (await request(app).post('/api/v1/keys').set('Authorization', `Bearer ${session}`).send({ name: 'k', mode: 'both' })).body.key.id;
});

test.after(() => mock.close());

const chat = (freeOnly) => request(app).post('/api/v1/playground/chat').set('Authorization', `Bearer ${session}`)
  .send({ keyId, model: 'paidmock/ok-two', freeOnly, messages: [{ role: 'user', content: 'hi' }] });

test('the playground free toggle keeps a paid-capable key on free models', async () => {
  const paid = await chat(false);
  assert.equal(paid.status, 200, JSON.stringify(paid.body));
  assert.equal(paid.headers['x-neorouter-model'], 'paidmock/ok-two');
  const free = await chat(true);
  assert.equal(free.status, 200, JSON.stringify(free.body));
  assert.equal(free.headers['x-neorouter-model'], 'freebie/ok-two', 'the same model on a free connection answers instead');
});
