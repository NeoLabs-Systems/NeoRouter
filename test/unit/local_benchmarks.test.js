'use strict';
require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const { migrate } = require('../../server/db/migrate');
const { getDatabase } = require('../../server/db/database');
const keys = require('../../server/services/router_keys/router_key_service');
const consent = require('../../server/services/benchmarks/consent');
const capture = require('../../server/services/benchmarks/capture');
const store = require('../../server/services/benchmarks/private_store');
const privacy = require('../../server/services/benchmarks/privacy');
const worker = require('../../server/services/benchmarks/worker');
const aggregates = require('../../server/services/benchmarks/aggregates');
const signal = require('../../server/services/benchmarks/signal');
const judge = require('../../server/services/benchmarks/evaluation/judge');
const pipeline = require('../../server/services/benchmarks/evaluation/pipeline');
const POLICY = require('../../server/services/benchmarks/policy');
migrate();
const db = getDatabase();
db.prepare("INSERT INTO users (id, username, email, password_hash, role) VALUES ('owner', 'owner', 'owner@example.test', 'x', 'admin'), ('other', 'other', 'other@example.test', 'x', 'user')").run();
const key = keys.create('owner', { name: 'capture', mode: 'free_only' }).key;
const canonical = { messages: [{ role: 'system', content: 'Answer the user accurately.' }, { role: 'user', content: 'Write a brief reply to sample.person@example.test.' }], max_tokens: 100 };
const entry = { canonicalId: 'public-model', ref: 1, definitionId: 'openai' };
const input = { userId: 'owner', keyId: key.id, canonical, message: { role: 'assistant', content: 'Contact sample.person@example.test.' }, finishReason: 'stop', entry };
const rows = () => db.prepare('SELECT * FROM benchmark_samples WHERE user_id = ?').all('owner');
function enable() {
  consent.saveGlobal(true); consent.saveUser('owner', { enabled: true, allowReplay: true, retentionDays: 7 }); consent.saveKey('owner', key.id, true);
}

test('all three gates default off; staging is sanitized and cryptographically user-isolated', async () => {
  assert.equal(await capture.capture(input), false);
  consent.saveGlobal(true);
  assert.equal(await capture.capture(input), false);
  consent.saveUser('owner', { enabled: true, allowReplay: true });
  assert.equal(await capture.capture(input), false);
  consent.saveKey('owner', key.id, true);
  assert.equal(await capture.capture(input), true);
  assert.equal(await capture.capture(input), false, 'exact task duplicates do not fill staging');
  const row = rows()[0];
  assert.equal(row.payload.includes('sample.person'), false);
  const privateData = store.decrypt('owner', row.payload);
  assert.equal(JSON.stringify(privateData).includes('sample.person@example.test'), false);
  assert.match(JSON.stringify(privateData), /EMAIL/);
  assert.throws(() => store.decrypt('other', row.payload));
  assert.deepEqual(store.counts('other'), {});
  assert.equal(await capture.capture({ ...input, internal: true }), false);
  assert.equal(await capture.capture({ ...input, peer: true }), false);
  assert.equal(await capture.capture({ ...input, entry: { ...entry, definitionId: 'neorouter_peer' } }), false);
  assert.equal(await capture.capture({ ...input, finishReason: 'length' }), false);
  assert.equal(await capture.capture({ ...input, finishReason: null }), false);
  assert.equal(await capture.capture({ ...input, signal: AbortSignal.abort() }), false);
});

test('revocation wins over asynchronous sanitization and prevents old tickets after re-enabling', async () => {
  enable();
  const oldTicket = consent.ticket('owner', key.id);
  const sanitize = privacy.sanitize;
  let unblock;
  privacy.sanitize = () => new Promise((resolve) => { unblock = resolve; });
  try {
    const pending = capture.capture(input);
    consent.saveKey('owner', key.id, false);
    unblock({ canonical, response: input.message });
    assert.equal(await pending, false);
    assert.equal(rows().length, 0);
  } finally { privacy.sanitize = sanitize; }
  consent.saveKey('owner', key.id, true);
  assert.equal(consent.allowed('owner', key.id, oldTicket), false);
  assert.equal(await capture.capture(input), true);
  consent.saveUser('owner', { enabled: false, allowReplay: false });
  assert.equal(rows().length, 0);
  assert.equal(await capture.capture(input), false);
});

test('retention and recovered worker leases never reveal or revive revoked samples', async () => {
  enable(); await capture.capture(input);
  const row = rows()[0];
  db.prepare("UPDATE benchmark_samples SET status = 'running', lease_until = ? WHERE id = ?").run(Date.now() - 1, row.id);
  assert.equal(worker.claim(Date.now()).id, row.id);
  consent.saveGlobal(false);
  assert.equal(store.checkpoint(row, { sample: {} }), false);
  assert.equal(rows().length, 0);
  enable(); await capture.capture(input);
  store.prune(Date.now() + 8 * 86400000);
  assert.equal(rows().length, 0);
});

test('versioned blind judges accept ties, abstain, reject low confidence and refuse extra text fields', () => {
  const message = (winner, confidence, extra = {}) => ({ content: JSON.stringify({ winner, confidence, reasons: ['correctness'], ...extra }) });
  assert.equal(judge.verdict(message('A', .95), true).score, 0);
  assert.equal(judge.verdict(message('tie', .95)).score, .5);
  assert.equal(judge.verdict(message('abstain', .95)), null);
  assert.equal(judge.verdict(message('A', .5)), null);
  assert.equal(judge.verdict(message('A', .95, { explanation: 'must not become global' })), null);
});

test('the evaluator calibrates independent judges and checks both answer orders before accepting evidence', async () => {
  enable(); await capture.capture(input);
  const row = rows()[0];
  const payload = store.decrypt('owner', row.payload);
  let calls = 0;
  const adapters = {
    candidates: () => [1, 2, 3, 4].map((ref) => ({ ref, canonicalId: `model-${ref}`, intelligence: ref / 10 })),
    call: async (_row, ref, request) => {
      calls++;
      if (request.messages[0].content === judge.REVIEW) return { content: JSON.stringify({ safe: true, deidentified: true, verifiable: true }) };
      if (request.messages[0].content !== judge.RUBRIC) return { role: 'assistant', content: `A complete answer from candidate ${ref}.` };
      const p = payload.progress;
      let score = 1, flipped;
      if (p.calibrations.length < 4) {
        const i = p.calibrations.length;
        score = p.cases[i % 2].expected; flipped = Math.floor(i / 2) % 2 === 0 ? p.flip : !p.flip;
      } else flipped = p.verdicts.length % 2 === 0 ? p.flip : !p.flip;
      return { content: JSON.stringify({ winner: (flipped ? 1 - score : score) === 1 ? 'A' : 'B', confidence: .95, reasons: ['correctness'] }) };
    },
  };
  let result;
  for (let step = 0; step < 20; step++) {
    result = await pipeline.step(row, payload, new AbortController().signal, adapters);
    if (result.state !== 'continue') break;
  }
  assert.equal(result.state, 'validated');
  assert.equal(calls, 11);
  assert.equal(new Set([...result.result.candidates.map((model) => model.canonicalId), ...result.result.judges]).size, 4);
  assert.equal(result.result.confidence, .95);
  const conflict = structuredClone(payload);
  conflict.progress.verdicts = [{ score: 0, confidence: .95 }];
  const invalid = await pipeline.step(row, conflict, new AbortController().signal, { ...adapters, call: async () => ({ content: JSON.stringify({ winner: conflict.progress.flip ? 'A' : 'B', confidence: .95, reasons: ['correctness'] }) }) });
  assert.equal(invalid.state, 'rejected');
});

test('only sufficient de-identified evidence is published; routing weight is small, task-specific and decays', async () => {
  enable();
  const ticket = consent.ticket('owner', key.id);
  const accepted = { candidates: [{ canonicalId: 'model-a' }, { canonicalId: 'model-b' }], judges: ['judge-a', 'judge-b'],
    task: 'coding', score: 1, confidence: .95, evaluatorVersion: judge.VERSION };
  const now = Date.now();
  for (let index = 0; index < POLICY.minSamples; index++) {
    assert.equal(aggregates.add({ user_id: 'owner', key_id: key.id, fingerprint: `distinct-task-${index}`, consent_json: JSON.stringify(ticket) }, accepted, now), true);
    if (index < POLICY.minSamples - 1) assert.deepEqual(aggregates.list(), []);
  }
  assert.equal(aggregates.list().length, 2);
  const global = JSON.stringify(aggregates.list());
  assert.equal(global.includes('owner'), false);
  assert.equal(global.includes('distinct-task'), false);
  const blended = signal.blend(.5, 'model-a', 'coding', now);
  assert.ok(blended > .5 && blended < .53);
  assert.equal(signal.blend(.5, 'model-a', 'general', now), .5);
  for (let index = 0; index < POLICY.minSamples; index++) aggregates.add({ user_id: 'owner', key_id: key.id, fingerprint: `tie-task-${index}`, consent_json: JSON.stringify(ticket) }, { ...accepted, task: 'reasoning', score: .5 }, now);
  assert.equal(signal.blend(.9, 'model-a', 'reasoning', now), .9, 'neutral ties preserve the external quality prior');
  assert.ok(signal.blend(.5, 'model-a', 'coding', now + 365 * 86400000) < blended);
  store.purge('other');
  assert.equal(aggregates.list().length, 4);
  store.purge('owner');
  assert.equal(aggregates.list().length, 0);
  assert.equal(signal.blend(.5, 'model-a', 'coding', now), .5);
});


test('mandatory capture sanitizes tool context and arguments without retaining a restoration vault', async () => {
  const email = 'tool.person@example.test';
  const sample = await privacy.sanitize('owner', {
    messages: [{ role: 'system', content: 'Propose the appropriate tool call.' }, { role: 'assistant', content: 'I can propose a call.', reasoning_content: 'Private hidden reasoning must not be captured.' }, { role: 'user', content: `Send a reminder to ${email}.` }, { role: 'tool', tool_call_id: 'prior', content: JSON.stringify({ recipient: email }) }],
    tools: [{ type: 'function', function: { name: 'notify', description: `Notify ${email}`, parameters: { type: 'object', properties: { recipient: { type: 'string' } } } } }],
    tool_choice: 'required',
  }, { role: 'assistant', content: null, tool_calls: [{ id: 'next', type: 'function', function: { name: 'notify', arguments: JSON.stringify({ recipient: email }) } }] });
  assert.equal(JSON.stringify(sample).includes(email), false);
  assert.match(sample.canonical.tools[0].function.description, /EMAIL/);
  assert.match(JSON.parse(sample.response.tool_calls[0].function.arguments).recipient, /EMAIL/);
  assert.equal(sample.vault, undefined);
  assert.equal(JSON.stringify(sample).includes('Private hidden reasoning'), false);
});

test('disabling user consent aborts an active worker step and cannot restore its deleted record', async () => {
  enable(); await capture.capture(input);
  const original = pipeline.step;
  let started;
  const ready = new Promise((resolve) => { started = resolve; });
  pipeline.step = async (_row, _payload, abortSignal) => {
    started();
    return new Promise((_resolve, reject) => abortSignal.addEventListener('abort', () => reject(abortSignal.reason), { once: true }));
  };
  try {
    const running = worker.tick();
    await ready;
    assert.equal(worker.health().active, true);
    consent.saveUser('owner', { enabled: false, allowReplay: false });
    await running;
    assert.equal(worker.health().active, false);
    assert.equal(rows().length, 0);
  } finally { pipeline.step = original; }
});


test('optional collection failures cannot break a successful foreground response', () => {
  const original = consent.ticket;
  consent.ticket = () => { throw new Error('Simulated private-store failure'); };
  try { assert.doesNotThrow(() => capture.schedule(input)); }
  finally { consent.ticket = original; }
});


test('tool IDs do not defeat task deduplication; known output contracts are graded objectively', () => {
  const canonical = (id) => ({ messages: [
    { role: 'assistant', tool_calls: [{ id, type: 'function', function: { name: 'lookup', arguments: '{"value":1}' } }] },
    { role: 'tool', tool_call_id: id, content: '{"value":2}' },
  ] });
  assert.equal(store.taskFingerprint('owner', canonical('first-id')), store.taskFingerprint('owner', canonical('second-id')));
  const objective = require('../../server/services/benchmarks/evaluation/objective');
  assert.equal(objective.valid({ response_format: { type: 'json_object' } }, { content: '[]' }), false);
  assert.equal(objective.valid({ tools: [{ function: { name: 'lookup' } }], tool_choice: 'required' },
    { tool_calls: [{ function: { name: 'invented_tool', arguments: '{}' } }] }), false);
});
