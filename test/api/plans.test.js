'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { createMockProvider } = require('../support/mock_provider');
const { getDatabase } = require('../../server/db/database');
const quota = require('../../server/services/quota/quota_service');

let app;
let mock;
let session;
let freeKey;
let paidKey;
let planAccount;

const api = (method, path) => request(app)[method](`/api/v1${path}`).set('Authorization', `Bearer ${session}`);
const chat = (secret) => request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${secret}`)
  .send({ model: 'auto', messages: [{ role: 'user', content: 'hi' }] });

test.before(async () => {
  app = createApp();
  mock = await createMockProvider({ models: ['ok-planned'], prices: { 'ok-planned': { prompt: '0.000002', completion: '0.000004' } } });
  session = (await request(app).post('/api/v1/auth/register').send({ username: 'planner', email: 'plan@example.test', password: 'correct-horse-battery' })).body.session.token;
  const created = await api('post', '/providers/accounts').send({ definitionId: 'custom_openai', slug: 'plan', baseUrl: mock.url, secret: 'good-key', billingMode: 'plan_paid' });
  assert.equal(created.status, 201, JSON.stringify(created.body));
  planAccount = created.body.account;
  assert.equal(planAccount.billingMode, 'plan_paid');
  freeKey = (await api('post', '/keys').send({ name: 'free', mode: 'free_only' })).body.secret;
  paidKey = (await api('post', '/keys').send({ name: 'paid', mode: 'paid', budgetUsd: 10, budgetPeriod: 'monthly' })).body.secret;
});

test.after(() => mock.close());

test('a plan model costs nothing extra, so free-only keys may use it', async () => {
  const models = (await api('get', '/models')).body.models;
  const planned = models.find((model) => model.id === 'plan/ok-planned');
  assert.equal(planned.isPlan, true);
  assert.equal(planned.isFree, false);
  const response = await chat(freeKey);
  assert.equal(response.status, 200, JSON.stringify(response.body));
  const row = getDatabase().prepare("SELECT cost_usd, is_free FROM requests WHERE model_label = 'plan/ok-planned' ORDER BY created_at DESC LIMIT 1").get();
  assert.deepEqual(row, { cost_usd: 0, is_free: 0 });
});

test('once a "plan, then pay per use" allowance is gone, requests are billed', async () => {
  quota.setState(`a:${planAccount.id}`, 'plan_requests', { limit: 100, remaining: 0, resetAt: Date.now() + 3_600_000 }, 'probe');
  const blocked = await chat(freeKey);
  assert.equal(blocked.status, 503, 'a free-only key cannot pay for the overage');
  assert.equal(blocked.body.error.details.rejected.not_free, 1);
  const billed = await chat(paidKey);
  assert.equal(billed.status, 200, JSON.stringify(billed.body));
  const row = getDatabase().prepare("SELECT cost_usd FROM requests WHERE model_label = 'plan/ok-planned' AND status = 'success' ORDER BY created_at DESC LIMIT 1").get();
  assert.ok(row.cost_usd > 0, 'the overage request has a cost');
});

test('a plan-only account stops at the limit instead of billing', async () => {
  const updated = await api('patch', `/providers/accounts/${planAccount.id}`).send({ billingMode: 'plan' });
  assert.equal(updated.status, 200, JSON.stringify(updated.body));
  assert.equal(updated.body.account.billingMode, 'plan');
  const response = await chat(paidKey);
  assert.equal(response.status, 503, JSON.stringify(response.body));
  assert.equal(response.body.error.details.rejected.plan_used_up, 1);
});

test('keys choose free, plan and paid models independently', async () => {
  // The plan account is plan-only again; reset its allowance.
  quota.setState(`a:${planAccount.id}`, 'plan_requests', { limit: 100, remaining: 50, resetAt: Date.now() + 3_600_000 }, 'probe');
  const planOnly = await api('post', '/keys').send({ name: 'plans', allowFree: false, allowPlan: true, allowPaid: false });
  assert.equal(planOnly.status, 201, JSON.stringify(planOnly.body));
  assert.deepEqual([planOnly.body.key.allowFree, planOnly.body.key.allowPlan, planOnly.body.key.allowPaid], [false, true, false]);
  assert.equal((await chat(planOnly.body.secret)).status, 200);

  const noPlans = await api('post', '/keys').send({ name: 'free', allowFree: true, allowPlan: false, allowPaid: false });
  const refused = await chat(noPlans.body.secret);
  assert.equal(refused.status, 503);
  assert.equal(refused.body.error.details.rejected.not_allowed_for_key, 1);

  const nothing = await api('post', '/keys').send({ name: 'none', allowFree: false, allowPlan: false, allowPaid: false });
  assert.equal(nothing.status, 400);
  assert.equal(nothing.body.error.code, 'NO_MODELS_ALLOWED');

  const updated = await api('patch', `/keys/${noPlans.body.key.id}`).send({ allowPaid: true });
  assert.equal(updated.body.key.mode, 'both', 'free and paid: falls back to free when the budget is used up');
});

test('a pay-per-use copy of a subscription model does not ride on its score', async () => {
  quota.setState(`a:${planAccount.id}`, 'plan_requests', { limit: 100, remaining: 80, resetAt: Date.now() + 3_600_000 }, 'probe');
  const reseller = await createMockProvider({ models: ['ok-planned'], prices: { 'ok-planned': { prompt: '0.00002', completion: '0.00008' } } });
  try {
    const created = await api('post', '/providers/accounts').send({ definitionId: 'custom_openai', slug: 'resell', baseUrl: reseller.url, secret: 'good-key', billingMode: 'paid' });
    assert.equal(created.status, 201, JSON.stringify(created.body));
    const both = (await api('post', '/keys').send({ name: 'both', allowFree: true, allowPlan: true, allowPaid: true, budgetUsd: 10, budgetPeriod: 'monthly' })).body.secret;
    const used = new Set();
    for (let index = 0; index < 8; index += 1) {
      const response = await chat(both);
      assert.equal(response.status, 200, JSON.stringify(response.body));
      used.add(response.headers['x-neorouter-model']);
    }
    assert.deepEqual([...used], ['plan/ok-planned'], 'the same model at a price only answers once the subscription cannot');
    // Spreading load evens out copies of one model at one price, not a paid copy and the subscription.
    const preview = (await api('post', '/routing/preview').send({ prompt: 'hi' })).body.candidates;
    const scoreOf = (id) => preview.find((item) => item.id === id).score;
    assert.ok(scoreOf('resell/ok-planned') < scoreOf('plan/ok-planned'), JSON.stringify(preview));
  } finally {
    await reseller.close();
  }
});
