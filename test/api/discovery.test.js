'use strict';
require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const metadata = require('../../server/services/catalog/metadata_service');

test('discovery news requires a session and the benchmark endpoint is gone', async () => {
  const app = createApp();
  assert.equal((await request(app).get('/api/v1/discovery/news')).status, 401);
  const token = (await request(app).post('/api/v1/auth/register').send({ username: 'reader', email: 'reader@example.test', password: 'correct-horse-battery' })).body.session.token;
  const api = (path) => request(app).get(`/api/v1/discovery/${path}`).set('Authorization', `Bearer ${token}`);
  assert.equal((await api('benchmarks')).status, 404);
  assert.equal((await api('news?limit=100000')).status, 400);
  assert.equal((await api('news?offset=-1')).status, 400);
  assert.equal((await api('news')).body.total, 0);
});

test('news picks up a new free variant that is free on a connected provider and only its base is measured', async () => {
  const app = createApp();
  metadata.resetForTests({
    artificialAnalysis: [
      { slug: 'hy4', name: 'Hy4', intelligenceIndex: 66, benchmarks: { gpqa: 0.9 } },
      { slug: 'weak-model', name: 'Weak model', intelligenceIndex: 20, benchmarks: { gpqa: 0.2 } },
    ],
    // OpenRouter sells the variant; only the weak model is free there.
    openrouter: [
      { id: 'tencent/hy4-flash', isFree: false, priceIn: 0.035, priceOut: 0.14 },
      { id: 'vendor/weak-model:free', isFree: true, priceIn: 0, priceOut: 0 },
    ],
    fetchedAt: { openrouter: new Date().toISOString() },
  });
  const { getDatabase } = require('../../server/db/database');
  const db = getDatabase();
  const token = (await request(app).post('/api/v1/auth/login').send({ account: 'reader', password: 'correct-horse-battery' })).body.session.token;
  const user = db.prepare("SELECT id FROM users WHERE username = 'reader'").get();
  db.prepare("INSERT INTO provider_accounts (id, user_id, definition_id, slug, label, billing_mode) VALUES ('nous-test', ?, 'nous', 'nous', 'Nous', 'free_tier')").run(user.id);
  db.prepare("INSERT INTO models (account_id, model_id, display_name, canonical_id, is_free, context_window) VALUES ('nous-test', 'tencent/hy4-flash:free', 'Hy4 Flash', 'hy4-flash', 1, 128000)").run();
  require('../../server/services/discovery/news_service').observe();
  const news = await request(app).get('/api/v1/discovery/news').set('Authorization', `Bearer ${token}`);
  const hy4 = news.body.items.find((item) => item.modelId === 'tencent/hy4-flash:free');
  assert.ok(hy4, JSON.stringify(news.body.items));
  assert.ok(hy4.score > 40 && hy4.score < 100, 'scored from its base, scaled down');
  assert.ok(!news.body.items.some((item) => item.name === 'Weak model'), 'below the median of free models');
});
