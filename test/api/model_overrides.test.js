'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { createMockProvider } = require('../support/mock_provider');

test('context and output limits can be pinned per model, and cleared back to the listing', async () => {
  const app = createApp();
  const provider = await createMockProvider({ models: ['ok-limits'] });
  try {
    const regRes = await request(app).post('/api/v1/auth/register').send({ username: 'limits', email: 'limits' + '@example.com', password: ['limits', 'password', '1'].join('-') });
    assert.equal(regRes.status, 201, JSON.stringify(regRes.body));
    const session = regRes.body.session.token;
    assert.equal((await request(app).post('/api/v1/providers/accounts').set('Authorization', `Bearer ${session}`)
      .send({ definitionId: 'custom_openai', baseUrl: provider.url, label: 'Limits host', secret: 'good-' + 'key', billingMode: 'free_tier' })).status, 201);
    const keyRes = await request(app).post('/api/v1/keys').set('Authorization', `Bearer ${session}`).send({ name: 'limits', mode: 'free_only' });
    const { secret } = keyRes.body;
    const ref = (await request(app).get('/api/v1/models').set('Authorization', `Bearer ${session}`)).body.models.find((model) => model.modelId === 'ok-limits').ref;

    const patch = (overrides) => request(app).patch(`/api/v1/models/${ref}`).set('Authorization', `Bearer ${session}`).send({ overrides });
    assert.equal((await patch({ contextWindow: 262144, maxOutput: 16384 })).status, 200);

    const entry = (await request(app).get(`/api/v1/models/${ref}`).set('Authorization', `Bearer ${session}`)).body.model;
    assert.equal(entry.contextWindow, 262144);
    assert.equal(entry.maxOutput, 16384);

    const listed = (await request(app).get('/v1/models').set('Authorization', `Bearer ${secret}`)).body.data.find((model) => model.id.endsWith('/ok-limits'));
    assert.equal(listed.context_length, 262144);
    assert.equal(listed.max_output_tokens, 16384);

    // Clearing falls back to what the listing reports (32768 context, no output cap).
    assert.equal((await patch({ contextWindow: null, maxOutput: null })).status, 200);
    const cleared = (await request(app).get('/v1/models').set('Authorization', `Bearer ${secret}`)).body.data.find((model) => model.id.endsWith('/ok-limits'));
    assert.equal(cleared.context_length, 32768);
    assert.equal(cleared.max_output_tokens, undefined);
  } finally {
    await provider.close();
  }
});
