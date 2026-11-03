'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { getDatabase } = require('../../server/db/database');
const { createMockProvider } = require('../support/mock_provider');
const experience = require('../../server/services/routing/task/experience');

const DAY = 86_400_000;
let app;
let upstream;
let session;

const api = (path, token = session) => request(app).get(`/api/v1${path}`).set('Authorization', `Bearer ${token}`);

test.before(async () => {
  app = createApp();
  upstream = await createMockProvider({ models: ['good-model', 'poor-model'] });
  session = (await request(app).post('/api/v1/auth/register').send({ username: 'learner', email: 'learner@example.test', password: 'a-long-test-password' })).body.session.token;
  const post = (path) => request(app).post(`/api/v1${path}`).set('Authorization', `Bearer ${session}`);
  await post('/providers/accounts').send({ definitionId: 'custom_openai', baseUrl: upstream.url, secret: 'good-key', billingMode: 'free_tier' });
});

test.after(async () => { await upstream.close(); });

const modelRef = (id) => getDatabase().prepare('SELECT id FROM models WHERE model_id = ?').get(id).id;

test('nothing learned yet is an empty answer, not an error', async () => {
  const response = await api('/usage/learning');
  assert.equal(response.status, 200);
  assert.equal(response.body.answers, 0);
  assert.deepEqual(response.body.best, []);
  assert.equal(response.body.firstTryNow, null);
});

test('the best model for a task weighs evidence, not one lucky answer', async () => {
  const good = modelRef('good-model');
  const poor = modelRef('poor-model');
  for (let i = 0; i < 40; i++) experience.recordOutcome(good, 'coding', i % 20 !== 0);
  experience.recordOutcome(poor, 'coding', true);
  for (let i = 0; i < 12; i++) experience.recordOutcome(poor, 'translation', i % 2 === 0);

  const { body } = await api('/usage/learning');
  assert.equal(body.tasksKnown, 2);
  assert.equal(body.modelsKnown, 2);
  const coding = body.best.find((item) => item.task === 'coding');
  assert.equal(coding.name, 'good-model');
  assert.equal(coding.answers, 41);
  assert.equal(body.map.tasks[0], 'coding');
  const cell = body.map.cells.find((item) => item.task === 'translation');
  assert.ok(cell.rate > 0.4 && cell.rate < 0.6, `rate ${cell.rate}`);
});

test('being good at the work beats merely answering: a reliable weak model is not the best', async () => {
  const db = getDatabase();
  const canonical = (id) => db.prepare('SELECT canonical_id FROM models WHERE model_id = ?').get(id).canonical_id;
  const score = db.prepare(`INSERT OR REPLACE INTO model_metadata (canonical_id, intelligence, intelligence_source, skills_json) VALUES (?, ?, ?, ?)`);
  score.run(canonical('good-model'), 0.3, 'artificial_analysis', JSON.stringify({ coding: 0.35 }));
  score.run(canonical('poor-model'), 0.8, 'calibrated', JSON.stringify({ coding: 0.9 }));
  const poor = modelRef('poor-model');
  for (let i = 0; i < 40; i++) experience.recordOutcome(poor, 'coding', i % 10 !== 0);

  const { body } = await api('/usage/learning');
  const coding = body.best.find((item) => item.task === 'coding');
  assert.equal(coding.name, 'poor-model', 'stronger in benchmarks, though it fails more often');
  assert.equal(coding.benchmarked, false, 'its score is an estimate, and says so');
  const weak = body.map.cells.find((item) => item.task === 'coding' && item.name === 'good-model');
  assert.ok(weak.rate > 0.9, `it does answer: ${weak.rate}`);
  assert.ok(weak.quality < 0.4, `but it is weak at coding: ${weak.quality}`);
  assert.equal(weak.benchmarked, true);
  assert.ok(weak.score < coding.score, 'one score takes both into account');
  const strong = body.map.cells.find((item) => item.task === 'coding' && item.name === 'poor-model');
  assert.ok(strong.score < strong.quality, 'and a track record of failures costs it');
});

test('first-try answers count per day, and a lost hedge is not a miss', async () => {
  const now = Date.now();
  const user = getDatabase().prepare('SELECT id FROM users WHERE username = ?').get('learner').id;
  const add = getDatabase().prepare(`INSERT INTO requests (id, user_id, created_at, api_format, requested_model, status, trail_json)
    VALUES (?, ?, ?, 'openai', 'auto', ?, ?)`);
  const missed = JSON.stringify([{ model: 'a/b', outcome: 'rate_limit' }, { model: 'c/d', outcome: 'success' }]);
  const raced = JSON.stringify([{ model: 'a/b', outcome: 'hedge_lost' }, { model: 'c/d', outcome: 'success' }]);
  add.run('old-1', user, now - 20 * DAY, 'success', missed);
  add.run('old-2', user, now - 20 * DAY, 'success', raced);
  add.run('mid-1', user, now - 10 * DAY, 'success', raced);
  add.run('new-1', user, now, 'success', raced);

  const { body } = await api('/usage/learning');
  assert.equal(body.rescued, 1);
  assert.equal(body.curve.length, 3);
  assert.equal(body.firstTryThen, 0.5);
  assert.equal(body.firstTryNow, 1);
});

test('limits it discovered and models it is resting are listed', async () => {
  const good = modelRef('good-model');
  getDatabase().prepare(`INSERT OR REPLACE INTO quota_limits (scope_key, metric, value, source, updated_at) VALUES (?, 'rpm', 12, 'learned', ?)`).run(`m:${good}`, Date.now());
  const { body } = await api('/usage/learning');
  const limit = body.discoveries.find((fact) => fact.kind === 'limit');
  assert.equal(limit.name, 'good-model');
  assert.equal(limit.value, 12);
});

test('another account sees none of it', async () => {
  await request(app).patch('/api/v1/admin/settings').set('Authorization', `Bearer ${session}`).send({ registrationOpen: true });
  const other = (await request(app).post('/api/v1/auth/register').send({ username: 'other', email: 'other@example.test', password: 'a-long-test-password' })).body.session.token;
  assert.ok(other, 'second account');
  const { body } = await api('/usage/learning', other);
  assert.equal(body.answers, 0);
  assert.deepEqual(body.discoveries, []);
  assert.deepEqual(body.curve, []);
});
