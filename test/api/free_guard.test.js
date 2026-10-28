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
let key;

test.before(async () => {
  app = createApp();
  mock = await createMockProvider({ models: ['cost-one', 'ok-two'] });
  session = (await request(app).post('/api/v1/auth/register').send({ username: 'guard', email: 'g@example.test', password: 'correct-horse-battery' })).body.session.token;
  await request(app).post('/api/v1/providers/accounts').set('Authorization', `Bearer ${session}`)
    .send({ definitionId: 'custom_openai', slug: 'mock', baseUrl: mock.url, secret: 'good-key', billingMode: 'free_tier' });
  key = (await request(app).post('/api/v1/keys').set('Authorization', `Bearer ${session}`).send({ name: 'k', mode: 'free_only' })).body.secret;
});

test.after(() => mock.close());

async function model(id) {
  const { models } = (await request(app).get('/api/v1/models').set('Authorization', `Bearer ${session}`)).body;
  return models.find((entry) => entry.id === id);
}

for (const stream of [false, true]) {
  test(`a free model that reports a charge is treated as paid from then on (stream: ${stream})`, async () => {
    if (stream) {
      // Reset the evidence from the non-streaming run.
      const target = await model('mock/cost-one');
      await request(app).patch(`/api/v1/models/${target.ref}`).set('Authorization', `Bearer ${session}`).send({ overrides: { billedAt: null } });
    }
    assert.equal((await model('mock/cost-one')).isFree, true);
    const first = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${key}`)
      .send({ model: 'mock/cost-one', stream, messages: [{ role: 'user', content: 'hi' }] });
    assert.equal(first.status, 200);
    const after = await model('mock/cost-one');
    assert.equal(after.isFree, false);
    assert.equal(after.freeReason, 'billed_observed');
    const second = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${key}`)
      .send({ model: 'mock/cost-one', messages: [{ role: 'user', content: 'hi' }] });
    assert.equal(second.status, 403, 'a free-only key may no longer use it');
  });
}
