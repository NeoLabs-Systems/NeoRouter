'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { getDatabase } = require('../../server/db/database');
const { createMockProvider } = require('../support/mock_provider');
const { RoutingPlan } = require('../../server/services/routing/router');

let app;
let dead;
let alive;
let session;
let secret;

const api = (method, path) => request(app)[method](`/api/v1${path}`).set('Authorization', `Bearer ${session}`);
const chat = (content) => request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${secret}`)
  .send({ model: 'auto', messages: [{ role: 'user', content }] });
const trailOf = (response) => String(response.headers['x-neorouter-trail'] || '').split(',').filter(Boolean);

test.before(async () => {
  app = createApp();
  dead = await createMockProvider({ models: ['fail-one', 'fail-two', 'fail-three', 'fail-four', 'fail-five'] });
  alive = await createMockProvider({ models: ['ok-alive'] });
  session = (await request(app).post('/api/v1/auth/register').send({ username: 'limits', email: 'limits@example.test', password: 'correct-horse-battery' })).body.session.token;
  for (const [slug, mock] of [['dead', dead], ['alive', alive]]) {
    const account = await api('post', '/providers/accounts').send({ definitionId: 'custom_openai', slug, baseUrl: mock.url, secret: 'good-key', billingMode: 'free_tier' });
    assert.equal(account.status, 201, JSON.stringify(account.body));
  }
  secret = (await api('post', '/keys').send({ name: 'limits', mode: 'both' })).body.secret;
  // The dead connection looks best, so it is always tried first.
  const db = getDatabase();
  const score = db.prepare('INSERT OR REPLACE INTO model_metadata (canonical_id, intelligence, intelligence_source, intelligence_confidence) VALUES (?, ?, ?, ?)');
  for (const row of db.prepare("SELECT canonical_id c, model_id m FROM models").all()) score.run(row.c, row.m.startsWith('fail-') ? 0.95 : 0.1, 'artificial_analysis', 0.9);
});

test.after(async () => {
  await dead.close();
  await alive.close();
});

test('one failing connection costs a request two attempts, then another gets a turn', async () => {
  const response = await chat('Write a function that adds two numbers.');
  assert.equal(response.status, 200, JSON.stringify(response.body));
  const trail = trailOf(response);
  assert.ok(trail.at(-1).endsWith('/ok-alive'), trail.join(','));
  assert.ok(trail.filter((id) => id.startsWith('dead/')).length <= 2, trail.join(','));
});

test('connection health names what the traffic shows', async () => {
  const health = (await api('get', '/usage/health')).body;
  assert.equal(health.requests, 1);
  assert.equal(health.failed, 0);
  assert.equal(health.shortOfCapacity, false);
  // Too few attempts to judge the connection on.
  assert.deepEqual(health.failing, []);
});

test('sending the same conversation again counts against the answer it repeats', async () => {
  const db = getDatabase();
  const votes = () => db.prepare('SELECT COALESCE(SUM(disliked), 0) n FROM model_task_stats').get().n;
  const content = 'Summarize the following text in one line: the cat sat on the mat.';
  assert.equal((await chat(content)).status, 200);
  const before = votes();
  assert.equal((await chat(content)).status, 200);
  assert.ok(votes() > before, 'one more dislike');
});

test('a model far too weak for the task keeps only part of its score', () => {
  const db = getDatabase();
  const key = db.prepare("SELECT * FROM router_keys WHERE name = 'limits'").get();
  const plan = new RoutingPlan({ userId: key.user_id, key, request: { messages: [{ role: 'user', content: 'Write a python function that reverses a list.' }] }, requestedModel: 'auto', needs: {}, inputTokens: 100 });
  const weak = plan.rank({ sample: false }).find((item) => item.entry.modelId === 'ok-alive');
  assert.ok(weak.details.capability < 0.12, `capability ${weak.details.capability}`);
  const { quota, penalty, load } = weak.details.factors;
  // At most 60 %: on a style that favours quality its own gate applies as well.
  assert.ok(weak.score / (weak.details.base * quota * penalty * load) <= 0.6 + 1e-9);
});
