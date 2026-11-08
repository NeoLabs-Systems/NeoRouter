'use strict';

// Settings → Usage → even out usage: shown with how much data there is, and
// it cannot be switched on before there is enough.
require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');

let app;
let token;
const api = (method, path, body) => request(app)[method](`/api/v1${path}`).set('Authorization', `Bearer ${token}`).send(body);

test.before(async () => {
  app = createApp();
  token = (await request(app).post('/api/v1/auth/register').send({ username: 'pace', email: 'pace@example.test', password: 'correct-horse-battery' })).body.session.token;
});

test('a new account sees how much data is still needed and cannot switch it on yet', async () => {
  const status = await api('get', '/routing/smoothing');
  assert.equal(status.status, 200);
  assert.equal(status.body.enabled, false);
  assert.equal(status.body.active, false);
  assert.equal(status.body.readiness.ready, false);
  assert.deepEqual(status.body.readiness.needed, { days: 7, activeDays: 5, requests: 100, coverage: 0.5 });
  const on = await api('put', '/routing/smoothing', { enabled: true });
  assert.equal(on.status, 409);
  assert.equal(on.body.error.code, 'NOT_ENOUGH_DATA');
  const off = await api('put', '/routing/smoothing', { enabled: false });
  assert.equal(off.status, 200);
  assert.equal(off.body.enabled, false);
});
