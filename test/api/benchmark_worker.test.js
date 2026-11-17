'use strict';
require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const http = require('node:http');
const crypto = require('node:crypto');
const { createApp } = require('../../server/app');
const { getDatabase } = require('../../server/db/database');
const { encryptString } = require('../../server/utils/crypto');
const keys = require('../../server/services/router_keys/router_key_service');
const consent = require('../../server/services/benchmarks/consent');
const capture = require('../../server/services/benchmarks/capture');
const worker = require('../../server/services/benchmarks/worker');
const aggregates = require('../../server/services/benchmarks/aggregates');
const { VERSION, REVIEW, RUBRIC } = require('../../server/services/benchmarks/evaluation/judge');
const POLICY = require('../../server/services/benchmarks/policy');

test('the autonomous worker reaches validated global aggregates through the router chat flow', async () => {
  createApp();
  const db = getDatabase();
  let calls = 0;
  const server = http.createServer(async (req, res) => {
    let body = '';
    for await (const chunk of req) body += chunk;
    const request = JSON.parse(body);
    calls++;
    let answer = 'Hello.';
    if (request.messages[0].content === REVIEW) answer = JSON.stringify({ safe: true, deidentified: true, verifiable: true });
    if (request.messages[0].content === RUBRIC) {
      const data = JSON.parse(request.messages[1].content);
      const question = data.task.messages[0].content;
      const sum = /equal to (\d+) \+ (\d+)/.exec(question);
      let winner = 'tie';
      if (sum) {
        const expected = Number(sum[1]) + Number(sum[2]);
        winner = JSON.parse(data.A.content).value === expected ? 'A' : 'B';
      } else if (question === 'What exact integer did a private random generator produce? No seed, state or output is supplied.') {
        winner = data.A.content === 'The value cannot be determined without the generator output or sufficient state.' ? 'A' : 'B';
      }
      answer = JSON.stringify({ winner, confidence: .99, reasons: [winner === 'tie' ? 'equivalent' : 'correctness'] });
    }
    res.writeHead(200, { 'content-type': 'application/json' });
    res.end(JSON.stringify({ id: 'evaluation-response', object: 'chat.completion', model: request.model,
      choices: [{ index: 0, message: { role: 'assistant', content: answer }, finish_reason: 'stop' }],
      usage: { prompt_tokens: 10, completion_tokens: 10, total_tokens: 20 },
    }));
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const originalRandom = crypto.randomInt;
  try {
    db.prepare("INSERT INTO users (id, username, email, password_hash, role) VALUES ('u', 'u', 'u@example.test', 'x', 'admin')").run();
    db.prepare(`INSERT INTO provider_accounts (id, user_id, definition_id, slug, label, base_url, secret_encrypted, billing_mode, status)
      VALUES ('a', 'u', 'custom_openai', 'own', 'Own', ?, ?, 'free_tier', 'healthy')`).run(`http://127.0.0.1:${server.address().port}/v1`, encryptString('test-provider-key'));
    for (const id of ['model-one', 'model-two', 'model-three', 'model-four']) {
      db.prepare(`INSERT INTO models (account_id, model_id, canonical_id, price_in, price_out, is_free, free_reason, context_window, max_output)
        VALUES ('a', ?, ?, 0, 0, 1, 'listed_zero_price', 32768, 4096)`).run(id, id.replaceAll('-', ''));
    }
    consent.saveGlobal(true);
    consent.saveUser('u', { enabled: true, allowReplay: true });
    const key = keys.create('u', { name: 'own evaluation', mode: 'free_only', benchmarkCollection: true }).key;
    const entry = require('../../server/services/catalog/catalog_service').listForUser('u')[0];
    for (let index = 0; index < POLICY.minSamples; index++) assert.equal(await capture.capture({
      userId: 'u', keyId: key.id, entry, canonical: { messages: [{ role: 'user', content: `Return a greeting for case ${index}.` }], max_tokens: 64 },
      message: { role: 'assistant', content: 'Hello.' }, finishReason: 'stop', task: 'general',
    }), true);
    // Deterministic selection keeps this integration test reproducible; production
    // continues using cryptographic random sampling and blind answer ordering.
    crypto.randomInt = (min, max) => max === undefined ? 0 : min;
    for (let step = 0; step < POLICY.minSamples * 16; step++) {
      await worker.tick();
      if (db.prepare("SELECT COUNT(*) count FROM benchmark_samples WHERE status = 'completed'").get().count === POLICY.minSamples) break;
    }
    assert.equal(db.prepare("SELECT COUNT(*) count FROM benchmark_samples WHERE status = 'completed'").get().count, POLICY.minSamples, JSON.stringify(worker.health()));
    assert.equal(calls, POLICY.minSamples * 11);
    assert.equal(worker.health().calls, calls);
    assert.equal(aggregates.list().length, 2);
    for (const result of aggregates.list()) { assert.equal(result.samples, POLICY.minSamples); assert.equal(result.score, .5); assert.equal(result.evaluatorVersion, VERSION); }
    // Replays run through the router's own chat flow, like any internal request.
    assert.ok(db.prepare('SELECT COUNT(*) count FROM requests').get().count > 0);
    assert.equal(db.prepare('SELECT COUNT(*) count FROM benchmark_samples WHERE payload IS NOT NULL').get().count, 0);
  } finally {
    crypto.randomInt = originalRandom;
    server.closeAllConnections();
    await new Promise((resolve) => server.close(resolve));
  }
});
