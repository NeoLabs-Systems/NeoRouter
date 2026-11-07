'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const quota = require('../../server/services/quota/quota_service');

// A stand-in for https://opencode.ai: the public listing, the Go usage route
// and the Chat Completions endpoint, so the account flow runs end to end
// without a subscription.
const chatCalls = [];
const realFetch = global.fetch;

function json(body, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } });
}

function stubFetch(url, init = {}) {
  const parsed = new URL(typeof url === 'string' ? url : url.url);
  const path = parsed.pathname;
  const headers = new Headers(init.headers || {});
  if (parsed.host !== 'opencode.ai') return realFetch(url, init);
  if (headers.get('authorization') !== 'Bearer sk-go-test') return json({ type: 'error', error: { type: 'AuthError', message: 'Unauthorized' } }, 401);
  if (path === '/inference/go/v1/usage') {
    return json({ usage: {
      rolling: { status: 'ok', percent: 37, resetsAt: '2026-11-07T14:12:03.000Z' },
      weekly: { status: 'ok', percent: 62, resetsAt: '2026-11-12T00:00:00.000Z' },
      monthly: { status: 'ok', percent: 12, resetsAt: '2026-12-01T00:00:00.000Z' },
    } });
  }
  if (path === '/inference/go/v1/models') {
    return json({ object: 'list', data: [
      { id: 'glm-5.3', name: 'GLM-5.3', type: 'chat', limit: { context: 1000000, output: 131072 }, cost: { text: { input: { price: 1.4 }, output: { price: 4.4 } } }, capabilities: { tools: { supported: true }, reasoning: { supported: true } } },
      { id: 'beam-free', name: 'Beam Free', type: 'chat', limit: { context: 1048576, output: 131072 }, cost: { text: { input: { price: 0 }, output: { price: 0 } } }, capabilities: { tools: { supported: true } } },
    ] });
  }
  if (path === '/inference/go/openai/v1/chat/completions') {
    if (!headers.get('x-opencode-session')) {
      return json({ type: 'error', error: { type: 'MissingSessionID', message: 'Request is missing x-opencode-session' } }, 400);
    }
    const body = JSON.parse(init.body);
    chatCalls.push({ model: body.model, headers: Object.fromEntries(headers) });
    return json({
      id: 'chatcmpl-1', object: 'chat.completion', created: 1, model: body.model,
      choices: [{ index: 0, message: { role: 'assistant', content: 'hello' }, finish_reason: 'stop' }],
      usage: { prompt_tokens: 5, completion_tokens: 1, cost: 0, opencode: { billing_source: 'go', allowance: 0.000012 } },
    });
  }
  return json({ error: 'not found' }, 404);
}

let app;
let session;
const api = (method, path) => request(app)[method](`/api/v1${path}`).set('Authorization', `Bearer ${session}`);

test.before(async () => {
  global.fetch = stubFetch;
  app = createApp();
  session = (await request(app).post('/api/v1/auth/register').send({ username: 'gopher', email: 'go@example.test', password: 'correct-horse-battery' })).body.session.token;
});

test.after(() => { global.fetch = realFetch; });

test('an OpenCode Go account is added as a plan and lists its models', async () => {
  const created = await api('post', '/providers/accounts').send({ definitionId: 'opencode_go', secret: 'sk-go-test' });
  assert.equal(created.status, 201, JSON.stringify(created.body));
  assert.equal(created.body.account.billingMode, 'plan');
  assert.equal(created.body.validation.status, 'healthy');
  const models = (await api('get', '/models')).body.models;
  assert.deepEqual(models.map((model) => model.id).sort(), ['opencode-go/beam-free', 'opencode-go/glm-5.3']);
  assert.equal(models.find((model) => model.id === 'opencode-go/glm-5.3').isPlan, true);
  // The free model Go includes for a limited time is free, not plan-priced.
  assert.equal(models.find((model) => model.id === 'opencode-go/beam-free').isFree, true);
});

test('the usage probe stores the three Go windows, used as shares left', async () => {
  const account = (await api('get', '/providers/accounts')).body.accounts[0];
  const probed = await api('post', `/providers/accounts/${account.id}/quota`);
  assert.equal(probed.status, 200, JSON.stringify(probed.body));
  const windows = quota.accountState(account.id).filter((item) => item.metric.startsWith('plan_'));
  const byMetric = Object.fromEntries(windows.map((item) => [item.metric, item.remaining]));
  assert.deepEqual(byMetric, { plan_5h: 63, plan_week: 38, plan_month: 88 });
});

test('a chat through the router carries the session id Go requires', async () => {
  const key = (await api('post', '/keys').send({ name: 'go', mode: 'paid', budgetUsd: 10, budgetPeriod: 'monthly' })).body.secret;
  const response = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${key}`)
    .send({ model: 'opencode_go/glm-5.3', messages: [{ role: 'system', content: 'You are a coding agent.' }, { role: 'user', content: 'hi' }] });
  assert.equal(response.status, 200, JSON.stringify(response.body));
  assert.equal(response.body.choices[0].message.content, 'hello');
  const call = chatCalls.at(-1);
  assert.equal(call.model, 'glm-5.3');
  assert.match(call.headers['x-opencode-session'], /^ses_[0-9a-f]{32}$/);
  assert.equal(call.headers['user-agent'], 'NeoRouter');
});

test('a revoked key is refused when the account is added', async () => {
  const created = await api('post', '/providers/accounts').send({ definitionId: 'opencode_go', secret: 'sk-revoked' });
  assert.equal(created.status, 422, JSON.stringify(created.body));
  assert.equal(created.body.error.code, 'KEY_REJECTED');
});
