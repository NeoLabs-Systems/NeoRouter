'use strict';

require('../support/env');
const { setTimeout: pause } = require('node:timers/promises');
const assert = require('node:assert/strict');
const test = require('node:test');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { getDatabase } = require('../../server/db/database');
const { createMockProvider } = require('../support/mock_provider');
const capture = require('../../server/services/benchmarks/capture');
const consent = require('../../server/services/benchmarks/consent');
const evidence = require('../../server/services/benchmarks/evidence');
const policy = require('../../server/services/benchmarks/policy');
const transport = require('../../server/services/benchmarks/transport');
const worker = require('../../server/services/benchmarks/worker');
const judge = require('../../server/services/benchmarks/evaluation/judge');
const objective = require('../../server/services/benchmarks/evaluation/objective');
const privacy = require('../../server/services/benchmarks/privacy');
const stats = require('../../server/services/routing/stats_store');
const quota = require('../../server/services/quota/quota_service');
const catalog = require('../../server/services/catalog/catalog_service');
const priority = require('../../server/services/routing/priority');

const models = ['echo-alpha', 'ok-beta', 'ok-gamma', 'ok-delta', 'cost-unsafe'];
const app = createApp();
let upstream;
let owner;
let other;
let key;

async function register(username) {
  const result = await request(app).post('/api/v1/auth/register').send({ username, email: `${username}@example.test`, password: 'correct-horse-battery' });
  assert.equal(result.status, 201, JSON.stringify(result.body));
  return { token: result.body.session.token, id: result.body.user.id };
}

const api = (actor, method, route) => request(app)[method](`/api/v1${route}`).set('Authorization', `Bearer ${actor.token}`);
const chat = (routerKey, content, stream = false, model = 'mock/echo-alpha') =>
  request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${routerKey}`)
    .send({ model, stream, messages: [{ role: 'user', content }] });
const totals = () => getDatabase().prepare('SELECT COALESCE(SUM(samples), 0) count FROM benchmark_evidence').get().count;
const analytics = () => getDatabase().prepare('SELECT COUNT(*) count FROM requests').get().count;

async function waitFor(check) {
  for (let i = 0; i < 150; i++) {
    if (check()) return;
    await pause(20);
  }
  assert.ok(check(), JSON.stringify(worker.health()));
}

test.before(async () => {
  upstream = await createMockProvider({ models, answer(body) {
    if (body.messages[0]?.content !== judge.request({}, [{}, {}], false).messages[0].content) return null;
    const { task, A, B } = JSON.parse(body.messages[1].content);
    const question = task.messages.at(-1).content;
    const arithmetic = question.match(/(\d+) \+ (\d+)/);
    let verdict = 'tie';
    if (arithmetic) verdict = JSON.parse(A.content).value === Number(arithmetic[1]) + Number(arithmetic[2]) ? 'A' : 'B';
    if (question.includes('private random generator')) verdict = A.content.includes('cannot be determined') ? 'A' : 'B';
    assert.ok(A && B);
    return JSON.stringify({ verdict, confidence: 'high' });
  } });
  owner = await register('benchmark-owner');
  assert.equal((await api(owner, 'patch', '/admin/settings').send({ registrationOpen: true })).status, 200);
  other = await register('benchmark-other');
  const provider = await api(owner, 'post', '/providers/accounts').send({
    definitionId: 'custom_openai', slug: 'mock', baseUrl: upstream.url, secret: 'good-key', billingMode: 'free_tier',
  });
  assert.equal(provider.status, 201, JSON.stringify(provider.body));
  const created = await api(owner, 'post', '/keys').send({ name: 'personal', mode: 'free_only' });
  assert.equal(created.status, 201, JSON.stringify(created.body));
  key = created.body;
});

test.after(async () => {
  worker.stop();
  await upstream.close();
});

test('all three gates are required and personal collection remains isolated', async () => {
  const before = await api(owner, 'get', '/benchmarks/settings');
  assert.equal(before.body.adminEnabled, false);
  assert.equal(before.body.enabled, false);
  assert.equal(key.key.benchmarkCollection, false);
  assert.equal((await api(owner, 'put', '/benchmarks/settings').send({ enabled: true })).status, 409);
  assert.equal((await api(owner, 'post', '/keys').send({ name: 'unauthorized', benchmarkCollection: true })).status, 409);
  assert.equal((await api(owner, 'get', '/keys')).body.keys.length, 1);
  assert.equal((await chat(key.secret, 'Give one example of a mammal.')).status, 200);
  assert.equal(capture.count(owner.id), 0);

  assert.equal((await api(owner, 'patch', '/admin/settings').send({ localBenchmarksEnabled: true })).status, 200);
  assert.equal((await api(owner, 'patch', `/keys/${key.key.id}`).send({ benchmarkCollection: true })).status, 409);
  assert.equal((await api(owner, 'put', '/benchmarks/settings').send({ enabled: true })).status, 200);
  assert.equal((await api(owner, 'post', '/keys').send({ name: 'pool', poolAccess: 'pool', benchmarkCollection: true })).status, 409);
  assert.equal((await chat(key.secret, 'Give one example of a mammal.')).status, 200);
  assert.equal(capture.count(owner.id), 0);
  assert.equal((await api(owner, 'patch', `/keys/${key.key.id}`).send({ benchmarkCollection: true })).status, 200);

  const address = 'private-benchmark@example.test';
  for (const stream of [false, true]) {
    assert.equal((await chat(key.secret, `Check the ${stream ? 'second' : 'first'} contact ${address}`, stream)).status, 200);
  }
  await waitFor(() => capture.count(owner.id) === 2);
  for (const sample of capture.staged.get(owner.id).values()) {
    assert.ok(!JSON.stringify(sample).includes(address));
    assert.equal(sample.source, 'echoalpha');
    assert.equal(sample.response.role, 'assistant');
  }
  assert.equal((await api(other, 'get', '/benchmarks/settings')).body.counts.pending, 0);
  assert.equal((await api(other, 'delete', '/benchmarks/collection')).status, 204);
  assert.equal(capture.count(owner.id), 2);
  const prior = consent.ticket(owner.id, key.key.id);
  assert.equal((await api(owner, 'put', '/benchmarks/settings').send({ enabled: false })).status, 200);
  assert.equal(capture.count(owner.id), 0);
  assert.equal((await api(owner, 'put', '/benchmarks/settings').send({ enabled: true })).status, 200);
  assert.notEqual(consent.ticket(owner.id, key.key.id), prior);
  const health = await api(owner, 'get', '/admin/settings');
  assert.equal(health.body.localBenchmarks.counts.pending, 0);
  assert.ok(!JSON.stringify(health.body.localBenchmarks).includes(address));
});

test('the worker shares the streaming router and quota, while metrics stay separate', async () => {
  const task = 'Explain the geometric properties of an equilateral triangle for private-benchmark@example.test';
  assert.equal((await chat(key.secret, task, true)).status, 200);
  await waitFor(() => capture.count(owner.id) === 1);
  const sample = [...capture.staged.get(owner.id).values()][0];
  assert.equal(sample.task, 'math', 'benchmark evidence follows the gateway task label even after redaction');
  assert.ok(!JSON.stringify(sample.canonical).includes('private-benchmark@example.test'));
  const entries = catalog.listForUser(owner.id, { capability: 'chat' });
  const accountId = entries[0].accountId;
  const reserved = priority.join([`account:${accountId}`], 'normal');
  assert.deepEqual(transport.candidates(sample, sample.canonical, 'auto:smart'), []);
  reserved();
  assert.equal(transport.eligible({ ...entries[0], freeReason: 'manual_free' }), false);
  assert.equal(transport.eligible({ ...entries[0], isPlan: true }), false);
  const charged = entries.find((entry) => entry.modelId === 'cost-unsafe');
  await assert.rejects(transport.call(sample, sample.canonical, { signal: new AbortController().signal, model: charged.id, ref: charged.ref }),
    { code: 'billed_model' });
  assert.equal(transport.eligible(catalog.getModel(owner.id, charged.ref)), false);
  const clock = Date.now();
  const learned = () => entries.map((entry) => {
    const state = stats.snapshot(entry.ref, clock);
    return [state.successes, state.failures, state.ttft_ms];
  });
  const before = learned();
  const start = upstream.calls.length;
  const requestCount = analytics();
  const recordQuota = test.mock.method(quota, 'recordRequest');
  worker.tick();
  await waitFor(() => totals() === 2);
  assert.equal(capture.count(owner.id), 0);
  assert.equal(analytics(), requestCount);
  assert.deepEqual(learned(), before);
  assert.equal(recordQuota.mock.callCount(), 7);
  assert.ok(upstream.calls.slice(start).every((call) => call.stream));
  assert.equal(new Set(sample.answers.map(({ entry }) => entry.canonicalId)).size, 2);
  const firstJudge = upstream.calls.at(-1).model;
  assert.ok(!sample.answers.some(({ entry }) => entry.modelId === firstJudge));
  assert.equal(getDatabase().prepare('SELECT DISTINCT evaluator FROM benchmark_evidence').get().evaluator, catalog.listForUser(owner.id).find((entry) => entry.modelId === firstJudge).canonicalId);
  test.mock.restoreAll();

  const noCapture = await api(owner, 'post', '/keys').send({ name: 'foreground', mode: 'free_only' });
  const startNext = upstream.calls.length;
  const [newSample, foreground] = await Promise.all([
    chat(key.secret, 'List another property of an equilateral triangle.', true),
    chat(noCapture.body.secret, 'Foreground request.'),
  ]);
  assert.equal(newSample.status, 200);
  assert.equal(foreground.status, 200);
  await waitFor(() => capture.count(owner.id) === 1);
  worker.tick();
  await waitFor(() => totals() === 4);
  assert.equal(upstream.calls.length - startNext, 5, 'calibration is reused across samples');
  assert.equal(analytics(), requestCount + 2);
  assert.ok(!JSON.stringify(getDatabase().prepare('SELECT * FROM benchmark_evidence').all()).includes(task));
});

test('a failed replay does not discard a completed answer and revocation drops pending work', async (t) => {
  assert.equal((await chat(key.secret, 'Describe the geometric meaning of an angle.', true)).status, 200);
  await waitFor(() => capture.count(owner.id) === 1);
  const [sample] = capture.staged.get(owner.id).values();
  const call = transport.call;
  let failed = false;
  let saved;
  t.mock.method(transport, 'call', async (...args) => {
    if (!failed && sample.answers.length === 1) {
      [saved] = sample.answers;
      failed = true;
      throw new transport.Deferred('provider_failure', { failed: true });
    }
    return call(...args);
  });
  worker.tick();
  await waitFor(() => sample.failures === 1);
  assert.equal(sample.answers.length, 1);
  assert.equal(sample.answers[0], saved);
  sample.nextAt = 0;
  worker.tick();
  await waitFor(() => capture.count(owner.id) === 0);
  assert.equal(sample.answers[0], saved);
  assert.equal(sample.answers.length, 2);

  const oldTicket = consent.ticket(owner.id, key.key.id);
  const entry = catalog.listForUser(owner.id).find((model) => model.modelId === 'echo-alpha');
  capture.schedule({
    userId: owner.id, keyId: key.key.id, ticket: oldTicket, entry, inputTokens: 5, finishReason: 'stop', task: 'chat',
    canonical: { messages: [{ role: 'user', content: 'Example for the revocation race' }] },
    message: { role: 'assistant', content: 'Example response' },
  });
  consent.saveKey(owner.id, key.key.id, false);
  consent.saveKey(owner.id, key.key.id, true);
  await pause(20);
  assert.equal(capture.count(owner.id), 0);
  assert.notEqual(consent.ticket(owner.id, key.key.id), oldTicket);
});

test('the mandatory shield preserves tool structure and removes private arguments and results', async () => {
  const address = 'private-tool@example.test';
  const call = { id: 'tool_call', type: 'function', function: { name: 'lookup', arguments: JSON.stringify({ contact: address }) } };
  const tools = [{ type: 'function', function: { name: 'lookup', parameters: { type: 'object' } } }];
  const clean = await privacy.sanitize(owner.id, {
    messages: [{ role: 'assistant', tool_calls: [call] }, { role: 'tool', tool_call_id: 'tool_call', content: address }],
    tools,
    tool_choice: 'required',
  }, { role: 'assistant', tool_calls: [call] });
  assert.ok(clean);
  assert.ok(!JSON.stringify(clean).includes(address));
  assert.deepEqual(clean.canonical.tools, tools);
  assert.equal(clean.canonical.messages.at(-1).tool_call_id, 'tool_call');
  assert.equal(objective.valid(clean.canonical, clean.response), true);
  assert.equal(objective.valid(clean.canonical, { role: 'assistant', content: 'Missing the required tool call.' }), false);
});

test('only sufficiently supported, fresh aggregates influence routing and owners can remove them', async () => {
  assert.equal(evidence.list().length, 0);
  const now = Date.now();
  const sample = { userId: owner.id, keyId: key.key.id, ticket: consent.ticket(owner.id, key.key.id), task: 'coding' };
  const candidates = [{ canonicalId: 'examplea' }, { canonicalId: 'exampleb' }];
  for (let i = 0; i < policy.minSamples; i++) {
    assert.equal(evidence.add(sample, candidates, { score: 1, confidence: 1, evaluator: 'objective', version: objective.VERSION }, now), true);
  }
  const result = await api(other, 'get', '/discovery/internal-benchmarks');
  assert.equal(result.status, 200);
  assert.equal(result.body.results.find((row) => row.model === 'examplea').samples, policy.minSamples);
  assert.ok(!JSON.stringify(result.body).includes(owner.id));
  assert.ok(evidence.blend(0.5, 'examplea', 'coding', now) > 0.5);
  assert.ok(evidence.blend(0.5, 'exampleb', 'coding', now) < 0.5);
  assert.equal(evidence.blend(0.5, 'examplea', 'coding', now + policy.evidenceLifetimeMs), 0.5);
  assert.equal((await api(owner, 'delete', '/benchmarks/collection')).status, 204);
  assert.equal(totals(), 0);
  assert.equal(evidence.blend(0.5, 'examplea', 'coding', now), 0.5);
});

test('judge maps answer order and separates ties from abstention', () => {
  const result = (verdict, confidence = 'high') => ({ content: JSON.stringify({ verdict, confidence }) });
  assert.equal(judge.verdict(result('A'), false), 1);
  assert.equal(judge.verdict(result('A'), true), 0);
  assert.equal(judge.verdict(result('tie'), true), 0.5);
  assert.equal(judge.verdict(result('abstain'), false), null);
  assert.equal(judge.verdict(result('B', 'low'), false), null);
});
