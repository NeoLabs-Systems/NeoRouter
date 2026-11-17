'use strict';
require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const metadata = require('../../server/services/catalog/metadata_service');

test('discovery requires a session and provides bounded, searchable public measurements', async () => {
  const app = createApp();
  metadata.resetForTests({ artificialAnalysis: [{ slug: 'measured-model', name: 'Measured model', intelligenceIndex: 50, benchmarks: { gpqa: 0.8 } }] });
  assert.equal((await request(app).get('/api/v1/discovery/benchmarks')).status, 401);
  const token = (await request(app).post('/api/v1/auth/register').send({ username: 'reader', email: 'reader@example.test', password: 'correct-horse-battery' })).body.session.token;
  const api = (path) => request(app).get(`/api/v1/discovery/${path}`).set('Authorization', `Bearer ${token}`);
  const result = await api('benchmarks?query=measured&limit=1');
  assert.equal(result.status, 200);
  assert.equal(result.body.total, 1);
  assert.equal(result.body.models[0].name, 'Measured model');
  assert.equal(result.body.models[0].isFree, false);
  assert.equal((await api('benchmarks?limit=100000')).status, 400);
  assert.equal((await api('benchmarks?offset=-1')).status, 400);
  assert.equal((await api('benchmarks?query=absent')).body.total, 0);
  assert.equal((await api('news')).body.total, 0);
  assert.equal(JSON.stringify(result.body).includes('secret_encrypted'), false);
});
