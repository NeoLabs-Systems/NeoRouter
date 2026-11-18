'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { createMockProvider } = require('../support/mock_provider');
const { getDatabase } = require('../../server/db/database');
const stats = require('../../server/services/routing/stats_store');

// A working model held back only by a preference (here: a style's speed
// limit) still answers when everything else fails: slow beats failing.
let app;
let provider;
let session;

const api = (method, path) => request(app)[method](`/api/v1${path}`).set('Authorization', `Bearer ${session}`);
const chat = (secret) => request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${secret}`)
  .send({ model: 'auto', messages: [{ role: 'user', content: 'hi' }] });

test.before(async () => {
  app = createApp();
  provider = await createMockProvider({ models: ['rl-fast', 'ok-slow'] });
  session = (await request(app).post('/api/v1/auth/register').send({ username: 'resort', email: 'resort@example.test', password: 'correct-horse-battery' })).body.session.token;
  const created = await api('post', '/providers/accounts').send({ definitionId: 'custom_openai', baseUrl: provider.url, secret: 'good-key', billingMode: 'free_tier' });
  assert.equal(created.status, 201, JSON.stringify(created.body));
  // The working model is far slower than the style allows.
  const { id } = getDatabase().prepare("SELECT id FROM models WHERE model_id = 'ok-slow'").get();
  for (let index = 0; index < 5; index += 1) stats.recordSuccess(id, { latencyMs: 12000, ttftMs: 9000, outputTokens: 100, generationMs: 3000 });
});

test.after(async () => provider.close());

test('a slow model that works answers when the fast one is rate-limited', async () => {
  const profile = await api('post', '/routing/profiles').send({
    name: 'Snappy', weights: { intelligence: 0.3, speed: 0.4, reliability: 0.2, cost: 0.1 }, constraints: { maxTtftMs: 2000 },
  });
  assert.equal(profile.status, 201, JSON.stringify(profile.body));
  const secret = (await api('post', '/keys').send({ name: 'snappy', profile: profile.body.profile.id })).body.secret;

  const response = await chat(secret);
  assert.equal(response.status, 200, JSON.stringify(response.body));
  assert.match(response.headers['x-neorouter-model'], /ok-slow/);
  assert.match(response.headers['x-neorouter-trail'], /rl-fast.*ok-slow/, 'the fast one first, as the style prefers');
  const row = getDatabase().prepare('SELECT trail_json FROM requests ORDER BY created_at DESC LIMIT 1').get();
  assert.equal(JSON.parse(row.trail_json).at(-1).lastResort, true, 'marked as a last resort');
});


test('a model that once needed more output room still answers when nothing else can', async () => {
  const { id } = getDatabase().prepare("SELECT id FROM models WHERE model_id = 'ok-slow'").get();
  stats.raiseMinOutputTokens(id, 16000);
  const secret = (await api('post', '/keys').send({ name: 'plain', mode: 'free_only' })).body.secret;
  const response = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${secret}`)
    .send({ model: 'auto', max_tokens: 4096, messages: [{ role: 'user', content: 'hi' }] });
  assert.equal(response.status, 200, JSON.stringify(response.body));
  assert.match(response.headers['x-neorouter-model'], /ok-slow/);
});
