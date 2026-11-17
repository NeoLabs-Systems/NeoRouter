'use strict';
require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const { migrate } = require('../../server/db/migrate');
const { getDatabase } = require('../../server/db/database');
const usage = require('../../server/services/usage/usage_service');
const keys = require('../../server/services/router_keys/router_key_service');

// Distinct provider credentials, not downstream router keys, are the divisor.
test('provider averages count distinct used connections and respect time/key filters', () => {
  migrate();
  const db = getDatabase();
  db.prepare("INSERT INTO users (id, username, email, password_hash, role) VALUES ('u', 'u', 'u@example.test', 'x', 'admin'), ('v', 'v', 'v@example.test', 'x', 'user')").run();
  for (const [id, definition] of [['a', 'openai'], ['b', 'openai'], ['unused', 'openai'], ['old', 'openai'], ['c', 'groq']]) {
    db.prepare("INSERT INTO provider_accounts (id, user_id, definition_id, slug, label, billing_mode) VALUES (?, 'u', ?, ?, ?, 'paid')").run(id, definition, id, id);
  }
  const first = keys.create('u', { name: 'first', mode: 'both' }).key.id;
  const second = keys.create('u', { name: 'second', mode: 'both' }).key.id;
  const now = Date.now();
  let sequence = 0;
  const record = (account, key, time = now, user = 'u') => usage.record({
    id: `${++sequence}`, user_id: user, router_key_id: key, created_at: time,
    api_format: 'openai', requested_model: 'auto', model_label: 'test/model', account_id: account,
    status: 'success', http_status: 200, input_tokens: 1, output_tokens: 1,
    cost_usd: 0, is_free: 0, latency_ms: 100, attempts: 1, stream: 0, trail_json: '[]',
  });
  for (let i = 0; i < 5; i++) record('a', first);
  for (let i = 0; i < 4; i++) record('b', second);
  for (let i = 0; i < 6; i++) record('c', first);
  record('old', first, now - 2 * 86_400_000);
  record('a', first, now, 'v');
  const all = usage.analytics('u', '24h', now);
  assert.equal(all.byProvider[0].key, 'OpenAI');
  assert.equal(all.byProvider[0].requests, 9);
  assert.equal(all.byProvider[0].accountCount, 2);
  assert.equal(all.byProvider[0].requestsPerAccount, 4.5);
  assert.equal(all.byProviderAverage[0].key, 'Groq');
  assert.equal(all.byProviderAverage[0].requestsPerAccount, 6);
  const filtered = usage.analytics('u', '24h', now, { keyIds: [first] });
  const openai = filtered.byProvider.find((row) => row.key === 'OpenAI');
  assert.equal(openai.accountCount, 1);
  assert.equal(openai.requestsPerAccount, 5);
  assert.equal(filtered.totals.requests, 11);
  record(null, first);
  const unknown = usage.analytics('u', '24h', now).byProvider.find((row) => row.key === 'test');
  assert.equal(unknown.accountCount, 0);
  assert.equal(unknown.requestsPerAccount, null, 'missing history must not invent a per-key average');
});
