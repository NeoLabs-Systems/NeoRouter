'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');

let app;
let admin;
let member;

async function register(name) {
  const response = await request(app).post('/api/v1/auth/register').send({ username: name, email: `${name}@example.test`, password: 'correct-horse-battery' });
  assert.equal(response.status, 201, JSON.stringify(response.body));
  return response.body.session.token;
}

const call = (token, method, path) => request(app)[method](`/api/v1/admin${path}`).set('Authorization', `Bearer ${token}`);

test.before(async () => {
  app = createApp();
  admin = await register('updater');
  await call(admin, 'patch', '/settings').send({ registrationOpen: true });
  member = await register('plain');
});

test('only admins see the update status', async () => {
  assert.equal((await call(member, 'get', '/update')).status, 403);
  const response = await call(admin, 'get', '/update');
  assert.equal(response.status, 200);
  assert.equal(response.body.job.state, 'idle');
  assert.match(response.body.channel, /^(stable|beta)$/);
  assert.equal(response.body.available, null);
});

test('a container cannot update itself', async () => {
  process.env.NEOROUTER_CONTAINER = '1';
  try {
    const response = await call(admin, 'post', '/update');
    assert.equal(response.status, 409);
    assert.equal(response.body.error.code, 'UPDATE_CONTAINER');
    assert.equal((await call(admin, 'get', '/update')).body.blockedReason, 'container');
  } finally {
    delete process.env.NEOROUTER_CONTAINER;
  }
});

test('an unknown channel is refused', async () => {
  const response = await call(admin, 'put', '/update/channel').send({ channel: 'nightly' });
  assert.equal(response.status, 400);
});
