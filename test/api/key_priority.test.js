'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { createMockProvider } = require('../support/mock_provider');
const concurrency = require('../../server/services/routing/concurrency');
const priority = require('../../server/services/routing/priority');

let app;
let upstream;
let session;
let account;
const secrets = {};

const api = (method, path) => request(app)[method](`/api/v1${path}`).set('Authorization', `Bearer ${session}`);
const chat = (level, content) => request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${secrets[level]}`)
  .send({ model: 'auto', messages: [{ role: 'user', content }] }).then((response) => response);
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

test.before(async () => {
  app = createApp();
  upstream = await createMockProvider({ models: ['slow-only'] });
  session = (await request(app).post('/api/v1/auth/register').send({ username: 'prio', email: 'prio@example.test', password: 'correct-horse-battery' })).body.session.token;
  account = (await api('post', '/providers/accounts').send({ definitionId: 'custom_openai', baseUrl: upstream.url, secret: 'good-key', billingMode: 'free_tier' })).body.account;
  for (const level of priority.LEVELS) {
    const created = await api('post', '/keys').send({ name: level, mode: 'free_only', priority: level });
    assert.equal(created.status, 201, JSON.stringify(created.body));
    assert.equal(created.body.key.priority, level);
    secrets[level] = created.body.secret;
  }
});

test.after(async () => {
  await upstream.close();
});

test('keys start at normal priority and can be changed on their own', async () => {
  const created = await api('post', '/keys').send({ name: 'plain', mode: 'free_only', rpmLimit: 7 });
  assert.equal(created.body.key.priority, 'normal');
  const changed = await api('patch', `/keys/${created.body.key.id}`).send({ priority: 'high' });
  assert.equal(changed.status, 200);
  assert.equal(changed.body.key.priority, 'high');
  assert.equal(changed.body.key.rpmLimit, 7, 'other settings stay');
  assert.equal((await api('patch', `/keys/${created.body.key.id}`).send({ priority: 'urgent' })).status, 400);
});

test('heldBack only yields to a higher priority, on what it waits for', () => {
  const entry = { ref: 'a/one', accountId: 'a' };
  const other = { ref: 'b/two', accountId: 'b' };
  const leave = priority.join(['model:a/one'], 'normal');
  assert.equal(priority.heldBack(entry, 'low'), true);
  assert.equal(priority.heldBack(entry, 'normal'), false, 'its own level keeps competing');
  assert.equal(priority.heldBack(entry, 'high'), false);
  assert.equal(priority.heldBack(other, 'low'), false, 'other models are not affected');
  leave();
  assert.equal(priority.heldBack(entry, 'low'), false);
  const leaveAccount = priority.join(['account:a'], 'high');
  assert.equal(priority.heldBack({ ref: 'a/three', accountId: 'a' }, 'normal'), true, 'a busy account holds all its models');
  leaveAccount();
});

test('when the account is busy a waiting high priority request goes before a low one that came first', async () => {
  concurrency.resetForTests();
  concurrency.onRateLimited({ accountId: account.id, definitionId: 'custom_openai' }, new Error('Too many concurrent requests'), { running: 2 });
  upstream.calls.length = 0;
  const running = chat('normal', 'first');
  await sleep(100);
  const low = chat('low', 'low');
  await sleep(50);
  const high = chat('high', 'high');
  for (const response of await Promise.all([running, low, high])) assert.equal(response.status, 200, JSON.stringify(response.body));
  assert.deepEqual(upstream.calls.map((call) => call.messages.at(-1).content), ['first', 'high', 'low']);
  assert.equal(priority.heldBack({ ref: 'any', accountId: account.id }, 'low'), false, 'nobody is left in the queue');
});
