'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { createMockProvider } = require('../support/mock_provider');

test('the models a router key sees name their provider, so copies of one model can be told apart', async () => {
  const app = createApp();
  const first = await createMockProvider({ models: ['ok-shared'] });
  const second = await createMockProvider({ models: ['ok-shared'] });
  try {
    const session = (await request(app).post('/api/v1/auth/register').send({ username: 'names', email: 'names@example.test', password: 'correct-horse-battery' })).body.session.token;
    const add = (url, label) => request(app).post('/api/v1/providers/accounts').set('Authorization', `Bearer ${session}`)
      .send({ definitionId: 'custom_openai', baseUrl: url, label, secret: 'good-key', billingMode: 'free_tier' });
    assert.equal((await add(first.url, 'Alpha host')).status, 201);
    assert.equal((await add(second.url, 'Beta host')).status, 201);
    const secret = (await request(app).post('/api/v1/keys').set('Authorization', `Bearer ${session}`).send({ name: 'names', mode: 'free_only' })).body.secret;

    const listed = (await request(app).get('/v1/models').set('Authorization', `Bearer ${secret}`)).body.data.filter((model) => model.id.includes('ok-shared'));
    assert.equal(listed.length, 2);
    const names = listed.map((model) => model.name);
    assert.equal(new Set(names).size, 2, `names differ: ${names.join(' | ')}`);
    assert.ok(names.some((name) => name.includes('Alpha host')) && names.some((name) => name.includes('Beta host')));

    const anthropic = (await request(app).get('/v1/models').set('Authorization', `Bearer ${secret}`).set('anthropic-version', '2023-06-01')).body.data
      .filter((model) => model.id.includes('ok-shared'));
    assert.equal(new Set(anthropic.map((model) => model.display_name)).size, 2);

    const autos = (await request(app).get('/v1/models').set('Authorization', `Bearer ${secret}`)).body.data.filter((model) => model.neorouter?.auto);
    assert.ok(autos.every((model) => !/\(Alpha|\(Beta/.test(model.name)), 'auto models are not provider copies');
  } finally {
    await first.close();
    await second.close();
  }
});
