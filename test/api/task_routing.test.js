'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { getDatabase } = require('../../server/db/database');
const { canonicalId } = require('../../server/services/catalog/model_identity');
const stats = require('../../server/services/routing/stats_store');
const { createMockProvider } = require('../support/mock_provider');

let app;
let mock;
let session;
let secret;

const api = (method, path) => request(app)[method](`/api/v1${path}`).set('Authorization', `Bearer ${session}`);
// A style that weighs only quality and does not explore, so the test sees
// the task effect alone (normal styles also draw among close candidates).
const chat = (content, headers = {}, model = 'auto:quality-only') => request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${secret}`).set(headers)
  .send({ model, messages: [{ role: 'user', content }] });

// Two models of the same overall intelligence, one strong at code, one at writing.
function setSkills(modelId, skills) {
  getDatabase().prepare(`INSERT INTO model_metadata (canonical_id, intelligence, intelligence_source, skills_json) VALUES (?, 0.6, 'test', ?)
    ON CONFLICT(canonical_id) DO UPDATE SET intelligence = 0.6, skills_json = excluded.skills_json`).run(canonicalId(modelId), JSON.stringify(skills));
}

test.before(async () => {
  app = createApp();
  mock = await createMockProvider({ models: ['ok-coder', 'ok-writer'] });
  session = (await request(app).post('/api/v1/auth/register').send({ username: 'tasks', email: 'tasks@example.test', password: 'correct-horse-battery' })).body.session.token;
  assert.equal((await api('post', '/providers/accounts').send({ definitionId: 'custom_openai', slug: 'mock', baseUrl: mock.url, secret: 'good-key', billingMode: 'free_tier' })).status, 201);
  secret = (await api('post', '/keys').send({ name: 'tasks', mode: 'free_only' })).body.secret;
  const style = await api('post', '/routing/profiles').send({ name: 'Quality only', weights: { intelligence: 1, speed: 0, reliability: 0, cost: 0 }, constraints: { exploration: 0.01, adaptive: false } });
  assert.equal(style.status, 201, JSON.stringify(style.body));
  setSkills('ok-coder', { coding: 0.95, instruction: 0.3, knowledge: 0.4 });
  setSkills('ok-writer', { coding: 0.3, instruction: 0.95, knowledge: 0.6 });
});

test.after(async () => mock.close());

test('the prompt type decides between models of equal overall intelligence', async () => {
  for (let i = 0; i < 4; i += 1) {
    stats.resetForTests();
    const code = await chat('Write a TypeScript function that debounces calls; handle edge cases and add unit tests');
    assert.equal(code.headers['x-neorouter-model'], 'mock/ok-coder');
    assert.match(code.headers['x-neorouter-task'], /^coding; difficulty=/);
    stats.resetForTests();
    const prose = await chat('Write a heartfelt wedding speech for my sister, warm in tone with a funny story');
    assert.equal(prose.headers['x-neorouter-model'], 'mock/ok-writer');
  }
  const row = getDatabase().prepare("SELECT task FROM requests WHERE model_label = 'mock/ok-coder' LIMIT 1").get();
  assert.equal(row.task, 'coding', 'the log keeps the task');
});

test('apps can name the task and give feedback on an answer', async () => {
  const hinted = await chat('Write a heartfelt wedding speech', { 'X-NeoRouter-Task': 'coding' });
  assert.match(hinted.headers['x-neorouter-task'], /^coding;/);
  const answer = await chat('Write a TypeScript function that debounces calls');
  const feedback = await request(app).post('/v1/feedback').set('Authorization', `Bearer ${secret}`).send({ request_id: answer.headers['x-request-id'], rating: 'bad' });
  assert.equal(feedback.status, 200, JSON.stringify(feedback.body));
  assert.equal(feedback.body.task, 'coding');
  const learned = getDatabase().prepare('SELECT disliked FROM model_task_stats WHERE task = ? AND disliked > 0').get('coding');
  assert.ok(learned.disliked > 0);
  const unknown = await request(app).post('/v1/feedback').set('Authorization', `Bearer ${secret}`).send({ request_id: 'nope', rating: 'good' });
  assert.equal(unknown.status, 404);
});
