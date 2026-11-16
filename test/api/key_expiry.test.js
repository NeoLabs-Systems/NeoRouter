'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { getDatabase } = require('../../server/db/database');

let app, session;
const api = (method, path, body) => request(app)[method](`/api/v1/keys${path}`).set('Authorization', `Bearer ${session}`).send(body);
const access = (secret) => request(app).get('/v1/models').set('Authorization', `Bearer ${secret}`);
test.before(async () => {
  app = createApp();
  session = (await request(app).post('/api/v1/auth/register').send({ username: 'expiry', email: 'expiry@example.test', password: 'correct-horse-battery' })).body.session.token;
});

test('expiration is validated, enforced, preserved on edits and explicitly renewable', async () => {
  const past = new Date(Date.now() - 60000).toISOString();
  const future = new Date(Date.now() + 86400000).toISOString();
  assert.equal((await api('post', '', { name: 'past', expiresAt: past })).status, 400);
  const created = await api('post', '', { name: 'temporary', expiresAt: future });
  assert.equal(created.status, 201);
  const { key, secret } = created.body;
  assert.equal(key.expiresAt, future);
  assert.equal(key.expired, false);
  assert.equal((await access(secret)).status, 200);
  getDatabase().prepare('UPDATE router_keys SET expires_at = ? WHERE id = ?').run(past, key.id);
  assert.equal((await access(secret)).status, 401);
  const edited = await api('patch', `/${key.id}`, { name: 'renamed' });
  assert.equal(edited.status, 200);
  assert.equal(edited.body.key.expiresAt, past);
  assert.equal(edited.body.key.expired, true);
  assert.equal((await access(secret)).status, 401, 'renaming does not reactivate the key');
  assert.equal((await api('patch', `/${key.id}`, { expiresAt: new Date(Date.now() - 120000).toISOString() })).status, 400);
  assert.equal((await api('patch', `/${key.id}`, { expiresAt: future })).status, 200);
  assert.equal((await access(secret)).status, 200, 'explicit extension preserves the secret');
  assert.equal((await api('patch', `/${key.id}`, { expiresAt: null })).body.key.expiresAt, null);
  await api('post', `/${key.id}/revoke`);
  await api('patch', `/${key.id}`, { expiresAt: new Date(Date.now() + 172800000).toISOString() });
  assert.equal((await access(secret)).status, 401, 'changing expiry never unrevokes a key');
});
