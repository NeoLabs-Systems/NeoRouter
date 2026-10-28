'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { createMockProvider } = require('../support/mock_provider');
const stats = require('../../server/services/routing/stats_store');

let app;
let mock;
let session;
let freeKey;
let accountId;

async function api(method, path, body) {
  const call = request(app)[method](`/api/v1${path}`).set('Authorization', `Bearer ${session}`);
  return body ? call.send(body) : call;
}

test.before(async () => {
  app = createApp();
  mock = await createMockProvider({ models: ['ok-alpha', 'ok-beta', 'ok-gamma', 'fail-delta', 'rl-epsilon', 'empty-zeta', 'tools-eta', 'text-embedding-3'] });
  const registered = await request(app).post('/api/v1/auth/register').send({ username: 'neo', email: 'neo@example.test', password: 'correct-horse-battery' });
  assert.equal(registered.status, 201);
  assert.equal(registered.body.user.role, 'admin');
  session = registered.body.session.token;
});

test.after(async () => {
  await mock.close();
});

test('second registration is closed until the admin opens it', async () => {
  const blocked = await request(app).post('/api/v1/auth/register').send({ username: 'other', email: 'o@example.test', password: 'correct-horse-battery' });
  assert.equal(blocked.status, 403);
  await api('patch', '/admin/settings', { registrationOpen: true });
  const allowed = await request(app).post('/api/v1/auth/register').send({ username: 'other', email: 'o@example.test', password: 'correct-horse-battery' });
  assert.equal(allowed.status, 201);
  assert.equal(allowed.body.user.role, 'user');
  await api('patch', '/admin/settings', { registrationOpen: false });
});

test('a rejected provider key is not saved', async () => {
  const response = await api('post', '/providers/accounts', { definitionId: 'custom_openai', baseUrl: mock.url, secret: 'bad-key' });
  assert.equal(response.status, 422);
  assert.equal(response.body.error.code, 'KEY_REJECTED');
});

test('adding a provider validates the key and fetches its models', async () => {
  const response = await api('post', '/providers/accounts', { definitionId: 'custom_openai', slug: 'mock', baseUrl: mock.url, secret: 'good-key', billingMode: 'free_tier' });
  assert.equal(response.status, 201, JSON.stringify(response.body));
  accountId = response.body.account.id;
  assert.equal(response.body.account.status, 'healthy');
  assert.equal(response.body.sync.models, 8);
  const models = await api('get', '/models');
  const ids = models.body.models.map((model) => model.id);
  assert.ok(ids.includes('mock/ok-alpha'));
  assert.ok(!ids.includes('mock/text-embedding-3'), 'embedding models are not chat models');
  assert.ok(models.body.models.every((model) => model.isFree && model.freeReason === 'free_tier_account'));
});

test('router key lists auto profiles and catalog models', async () => {
  const created = await api('post', '/keys', { name: 'free', mode: 'free_only' });
  assert.equal(created.status, 201);
  freeKey = created.body.secret;
  assert.match(freeKey, /^nrt_[0-9a-f]{8}_/);
  const models = await request(app).get('/v1/models').set('Authorization', `Bearer ${freeKey}`);
  const ids = models.body.data.map((model) => model.id);
  assert.ok(ids.includes('auto'));
  assert.ok(ids.includes('auto:fast'));
  assert.ok(ids.includes('mock/ok-beta'));
});

test('explicit model, non-streaming OpenAI chat', async () => {
  const response = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${freeKey}`)
    .send({ model: 'mock/ok-alpha', messages: [{ role: 'user', content: 'hi' }] });
  assert.equal(response.status, 200, JSON.stringify(response.body));
  assert.equal(response.body.choices[0].message.content, 'hello from ok-alpha');
  assert.equal(response.headers['x-neorouter-model'], 'mock/ok-alpha');
  assert.equal(response.body.usage.completion_tokens, 6);
});

test('explicit failing model falls back to nothing and reports the trail', async () => {
  const response = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${freeKey}`)
    .send({ model: 'mock/fail-delta', messages: [{ role: 'user', content: 'hi' }] });
  assert.equal(response.status, 502);
  assert.equal(response.body.error.code, 'ALL_ATTEMPTS_FAILED');
  assert.equal(response.body.error.details.trail[0].outcome, 'server');
});

test('auto routing skips failing, rate-limited and empty models', async () => {
  for (let index = 0; index < 12; index += 1) {
    const response = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${freeKey}`)
      .send({ model: 'auto', messages: [{ role: 'user', content: `question ${index}` }] });
    assert.equal(response.status, 200, JSON.stringify(response.body));
    const message = response.body.choices[0].message;
    assert.ok(/^hello from /.test(message.content || '') || message.tool_calls?.length, JSON.stringify(message));
  }
  const models = (await api('get', '/models')).body.models;
  const rateLimited = models.find((model) => model.id === 'mock/rl-epsilon');
  if (rateLimited.stats.lastFailureAt) assert.equal(rateLimited.availability.available, false, 'rate-limited model is cooling down');
});

test('streaming OpenAI chat replays SSE chunks and ends with [DONE]', async () => {
  const response = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${freeKey}`)
    .send({ model: 'mock/ok-beta', stream: true, messages: [{ role: 'user', content: 'hi' }] });
  assert.equal(response.status, 200);
  assert.match(response.headers['content-type'], /text\/event-stream/);
  const text = response.text;
  assert.match(text, /"content":"hello "/);
  assert.match(text, /data: \[DONE\]/);
});

test('streaming falls back when a stream ends without output', async () => {
  const response = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${freeKey}`)
    .send({ model: 'mock/empty-zeta', stream: true, messages: [{ role: 'user', content: 'hi' }] });
  assert.equal(response.status, 502);
  assert.equal(response.body.error.details.trail[0].outcome, 'empty');
});

test('a reply with reasoning but no answer counts as empty', async () => {
  const added = await api('post', '/providers/accounts', { definitionId: 'custom_openai', slug: 'thinker', baseUrl: mock.url, secret: 'good-key', billingMode: 'free_tier', options: { manualModels: ['think-only'] } });
  assert.equal(added.status, 201);
  const response = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${freeKey}`)
    .send({ model: 'thinker/think-only', messages: [{ role: 'user', content: 'hi' }] });
  assert.equal(response.status, 502);
  assert.equal(response.body.error.details.trail[0].outcome, 'empty');
  await api('delete', `/providers/accounts/${added.body.account.id}`);
});

test('reasoning that exhausts max_tokens falls back and is learned', async () => {
  const added = await api('post', '/providers/accounts', { definitionId: 'custom_openai', slug: 'reasoner', baseUrl: mock.url, secret: 'good-key', billingMode: 'free_tier', options: { manualModels: ['trunc-thinker'] } });
  const body = { model: 'reasoner/trunc-thinker', max_tokens: 50, messages: [{ role: 'user', content: 'hi' }] };
  const first = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${freeKey}`).send(body);
  assert.equal(first.body.error.details.trail[0].outcome, 'truncated');
  const models = (await api('get', '/models')).body.models;
  const learned = models.find((model) => model.id === 'reasoner/trunc-thinker');
  const db = require('../../server/db/database').getDatabase();
  assert.equal(db.prepare('SELECT min_output_tokens FROM model_stats WHERE model_ref = ?').get(learned.ref).min_output_tokens, 100);
  await api('delete', `/providers/accounts/${added.body.account.id}`);
});

test('Anthropic Messages endpoint, plain and streaming', async () => {
  const plain = await request(app).post('/v1/messages').set('x-api-key', freeKey).set('anthropic-version', '2023-06-01')
    .send({ model: 'mock/ok-gamma', max_tokens: 50, system: 'be brief', messages: [{ role: 'user', content: 'hi' }] });
  assert.equal(plain.status, 200, JSON.stringify(plain.body));
  assert.equal(plain.body.type, 'message');
  assert.equal(plain.body.content[0].text, 'hello from ok-gamma');
  assert.equal(plain.body.stop_reason, 'end_turn');
  const streamed = await request(app).post('/v1/messages').set('x-api-key', freeKey)
    .send({ model: 'mock/ok-gamma', max_tokens: 50, stream: true, messages: [{ role: 'user', content: 'hi' }] });
  assert.match(streamed.text, /event: message_start/);
  assert.match(streamed.text, /"type":"text_delta","text":"hello "/);
  assert.match(streamed.text, /event: message_stop/);
});

test('Anthropic tool use roundtrip', async () => {
  const response = await request(app).post('/v1/messages').set('x-api-key', freeKey)
    .send({
      model: 'mock/tools-eta', max_tokens: 50,
      tools: [{ name: 'lookup', description: 'Look up', input_schema: { type: 'object', properties: { q: { type: 'string' } } } }],
      messages: [{ role: 'user', content: 'search' }],
    });
  assert.equal(response.status, 200, JSON.stringify(response.body));
  const tool = response.body.content.find((block) => block.type === 'tool_use');
  assert.deepEqual(tool.input, { q: 'x' });
  assert.equal(response.body.stop_reason, 'tool_use');
});

test('a required tool call that never comes triggers fallback', async () => {
  const response = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${freeKey}`)
    .send({
      model: 'mock/ok-alpha', tool_choice: 'required',
      tools: [{ type: 'function', function: { name: 'lookup', parameters: { type: 'object', properties: {} } } }],
      messages: [{ role: 'user', content: 'use the tool' }],
    });
  assert.equal(response.status, 502);
  assert.equal(response.body.error.details.trail[0].outcome, 'invalid_output');
});

test('Responses API, plain and streaming', async () => {
  const plain = await request(app).post('/v1/responses').set('Authorization', `Bearer ${freeKey}`)
    .send({ model: 'mock/ok-alpha', instructions: 'be brief', input: 'hi' });
  assert.equal(plain.status, 200, JSON.stringify(plain.body));
  assert.equal(plain.body.object, 'response');
  assert.equal(plain.body.output_text, 'hello from ok-alpha');
  const streamed = await request(app).post('/v1/responses').set('Authorization', `Bearer ${freeKey}`)
    .send({ model: 'mock/ok-alpha', input: [{ role: 'user', content: [{ type: 'input_text', text: 'hi' }] }], stream: true });
  assert.match(streamed.text, /event: response.created/);
  assert.match(streamed.text, /event: response.output_text.delta/);
  assert.match(streamed.text, /event: response.completed/);
});

test('parallel burst is spread across several healthy models', async () => {
  for (const ref of (await api('get', '/models')).body.models.map((model) => model.ref)) stats.clearCooldown(ref);
  mock.calls.length = 0;
  await Promise.all(Array.from({ length: 30 }, (_, index) => request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${freeKey}`)
    .send({ model: 'auto', messages: [{ role: 'user', content: `burst ${index}` }] })));
  const served = new Set(mock.calls.filter((call) => call.model.startsWith('ok-') || call.model.startsWith('tools-')).map((call) => call.model));
  assert.ok(served.size >= 2, `expected load on at least two models, got ${[...served].join(', ')}`);
});

test('free-only key never routes to paid models', async () => {
  await api('patch', `/providers/accounts/${accountId}`, { billingMode: 'paid' });
  const response = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${freeKey}`)
    .send({ model: 'auto', messages: [{ role: 'user', content: 'hi' }] });
  assert.equal(response.status, 503);
  assert.equal(response.body.error.code, 'NO_MODEL_AVAILABLE');
  assert.ok(response.body.error.details.rejected.not_free > 0);
  await api('patch', `/providers/accounts/${accountId}`, { billingMode: 'free_tier' });
});

test('explicit paid model on a free-only key is a clear 403', async () => {
  await api('patch', `/providers/accounts/${accountId}`, { billingMode: 'paid' });
  const response = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${freeKey}`)
    .send({ model: 'mock/ok-alpha', messages: [{ role: 'user', content: 'hi' }] });
  assert.equal(response.status, 403);
  assert.equal(response.body.error.code, 'MODEL_NOT_ALLOWED');
  await api('patch', `/providers/accounts/${accountId}`, { billingMode: 'free_tier' });
});

test('budget keys stop at the limit and reject unknown prices', async () => {
  const models = (await api('get', '/models')).body.models;
  const alpha = models.find((model) => model.id === 'mock/ok-alpha');
  await api('patch', `/providers/accounts/${accountId}`, { billingMode: 'paid' });
  await api('patch', `/models/${alpha.ref}`, { overrides: { priceIn: 100000, priceOut: 100000 } });
  const created = await api('post', '/keys', { name: 'paid', mode: 'paid', budgetUsd: 3, allowedModels: ['mock/ok-*'] });
  const paidKey = created.body.secret;
  const first = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${paidKey}`)
    .send({ model: 'auto', max_tokens: 10, messages: [{ role: 'user', content: 'hi' }] });
  assert.equal(first.status, 200, JSON.stringify(first.body));
  assert.equal(first.headers['x-neorouter-model'], 'mock/ok-alpha', 'models without a price are excluded from budget keys');
  const key = (await api('get', '/keys')).body.keys.find((item) => item.name === 'paid');
  assert.ok(key.spentUsd > 1.7 && key.spentUsd < 1.9, `spent ${key.spentUsd}`);
  const second = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${paidKey}`)
    .send({ model: 'auto', max_tokens: 10, messages: [{ role: 'user', content: 'hi' }] });
  assert.equal(second.status, 503, 'remaining budget cannot cover the worst-case cost');
  await api('patch', `/providers/accounts/${accountId}`, { billingMode: 'free_tier' });
});

test('malformed JSON and oversized context get precise errors', async () => {
  const bad = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${freeKey}`).set('Content-Type', 'application/json').send('{"model": "auto", "messages": [');
  assert.equal(bad.status, 400);
  assert.equal(bad.body.error.code, 'INVALID_JSON');
  const huge = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${freeKey}`)
    .send({ model: 'auto', messages: [{ role: 'user', content: 'word '.repeat(200_000) }] });
  assert.equal(huge.status, 400);
  assert.equal(huge.body.error.code, 'CONTEXT_TOO_LARGE');
});

test('request logs and usage summary record metadata only', async () => {
  const logs = (await api('get', '/usage/logs?limit=5')).body.logs;
  assert.ok(logs.length > 0);
  assert.ok(!('messages' in logs[0]));
  const summary = (await api('get', '/usage/summary?range=24h')).body;
  assert.ok(summary.totals.requests > 10);
  const analytics = (await api('get', '/usage/analytics?range=7d')).body;
  assert.equal(analytics.series.length, 28);
  assert.ok(analytics.byModel.length > 0 && analytics.byProvider[0].key === 'mock');
  assert.ok(analytics.attemptOutcomes.some((item) => item.key === 'success'));
  const quotas = (await api('get', '/usage/quotas')).body;
  assert.ok(quotas.accounts.length >= 1);
});

test('routing preview ranks candidates without calling providers', async () => {
  const before = mock.calls.length;
  const preview = (await api('post', '/routing/preview', { profile: 'smart', prompt: 'hello' })).body;
  assert.equal(preview.profile.id, 'smart');
  assert.ok(preview.candidates.length > 0);
  assert.equal(mock.calls.length, before);
});

test('web files are served for revalidation so new releases are picked up', async () => {
  const response = await request(app).get('/');
  if (response.status === 200) assert.equal(response.headers['cache-control'], 'no-cache');
});
