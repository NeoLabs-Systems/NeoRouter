'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const { migrate } = require('../../server/db/migrate');
const { getDatabase } = require('../../server/db/database');
const scoring = require('../../server/services/routing/scoring');
const stats = require('../../server/services/routing/stats_store');
const learning = require('../../server/services/routing/learning');
const profiles = require('../../server/services/routing/profiles');
const budget = require('../../server/services/budget/budget_service');
const { ProviderError, KINDS, classifyHttpError, learnFromMessage } = require('../../server/providers/provider_error');
const { parseRateLimitHeaders, parseReset } = require('../../server/services/quota/header_parser');
const { sanitizeSchema } = require('../../server/providers/adapters/google_gemini');

migrate();
const db = getDatabase();
db.prepare("INSERT INTO users (id, username, email, password_hash, role) VALUES ('u', 'u', 'u@x.test', 'x', 'admin')").run();
db.prepare("INSERT INTO provider_accounts (id, user_id, definition_id, slug, label, billing_mode) VALUES ('a', 'u', 'groq', 'groq', 'Groq', 'free_tier')").run();
let nextRef = 1;

function entry(overrides = {}) {
  const ref = nextRef++;
  db.prepare("INSERT INTO models (id, account_id, model_id, canonical_id) VALUES (?, 'a', ?, ?)").run(ref, `m${ref}`, `m${ref}`);
  return {
    ref, id: `groq/m${ref}`, modelId: `m${ref}`, accountId: 'a', definitionId: 'groq', isFree: false, priceIn: 1, priceOut: 2,
    intelligence: 0.5, speedPriorTps: null, ...overrides,
  };
}

const fixedRandom = () => 0.5;

test('presets favour the dimension they are named after', () => {
  const smart = entry({ intelligence: 0.95, priceIn: 10, priceOut: 40 });
  const cheap = entry({ intelligence: 0.3, isFree: true });
  const score = (item, preset) => scoring.score(item, { weights: profiles.PRESETS[preset].weights, sample: false, random: fixedRandom }).score;
  assert.ok(score(smart, 'smart') > score(cheap, 'smart'));
  assert.ok(score(cheap, 'cheap') > score(smart, 'cheap'));
});

test('failures lower reliability and successes restore it', () => {
  const model = entry();
  const before = scoring.reliability(stats.snapshot(model.ref), { sample: false });
  for (let index = 0; index < 5; index += 1) learning.onFailure(model, new ProviderError(KINDS.SERVER, 'boom'));
  const after = scoring.reliability(stats.snapshot(model.ref), { sample: false });
  assert.ok(after < before);
  assert.equal(stats.availability(model.ref).reason, 'circuit_open', 'three failures in 15 minutes open the breaker');
  stats.clearCooldown(model.ref);
  for (let index = 0; index < 30; index += 1) learning.onSuccess(model, { latencyMs: 500, ttftMs: 100, outputTokens: 100, generationMs: 400 });
  assert.ok(scoring.reliability(stats.snapshot(model.ref), { sample: false }) > after);
});

test('quota exhaustion escalates the cooldown ladder', () => {
  const model = entry();
  const now = Date.now();
  const durations = [];
  for (let step = 0; step < 5; step += 1) {
    const at = now + step * 1000;
    learning.onFailure(model, new ProviderError(KINDS.QUOTA_EXHAUSTED, 'daily quota exceeded'), { now: at });
    durations.push(stats.snapshot(model.ref, at).cooldown_until - at);
  }
  assert.deepEqual(durations, [...learning.QUOTA_LADDER_MS, learning.QUOTA_LADDER_MS[3]]);
});

test('rate limits respect Retry-After and context errors only teach the limit', () => {
  const model = entry();
  const now = Date.now();
  learning.onFailure(model, new ProviderError(KINDS.RATE_LIMIT, 'slow down', { retryAfterMs: 300_000 }), { now });
  assert.equal(stats.snapshot(model.ref, now).cooldown_until, now + 300_000);
  const other = entry();
  learning.onFailure(other, new ProviderError(KINDS.CONTEXT_LENGTH, 'too long'), { estimatedTokens: 10000 });
  assert.equal(stats.snapshot(other.ref).failures, 0);
  assert.equal(db.prepare('SELECT context_window FROM models WHERE id = ?').get(other.ref).context_window, 9000);
});

test('penalties decay over time', () => {
  const model = entry();
  const now = Date.now();
  learning.onFailure(model, new ProviderError(KINDS.SERVER, 'x'), { now });
  const fresh = stats.snapshot(model.ref, now).penalty;
  const later = stats.snapshot(model.ref, now + 30 * 60_000).penalty;
  assert.ok(later < fresh / 10);
});

test('HTTP errors are classified for routing', () => {
  assert.equal(classifyHttpError(429, { error: { message: 'Rate limit reached for requests per minute' } }).kind, KINDS.RATE_LIMIT);
  assert.equal(classifyHttpError(429, { error: { message: 'You exceeded your daily quota' } }).kind, KINDS.QUOTA_EXHAUSTED);
  assert.equal(classifyHttpError(400, { error: { message: "This model's maximum context length is 8192 tokens" } }).kind, KINDS.CONTEXT_LENGTH);
  assert.equal(classifyHttpError(401, { error: { message: 'bad key' } }).kind, KINDS.AUTH);
  assert.equal(classifyHttpError(503, 'overloaded').kind, KINDS.SERVER);
  assert.deepEqual(learnFromMessage('Rate limit reached: limit: 1,000 requests per day'), { metric: 'rpd', value: 1000 });
});

test('rate-limit headers parse durations and suffixed periods', () => {
  const now = 1_000_000;
  assert.equal(parseReset('1m30s', now), now + 90_000);
  assert.equal(parseReset('6ms', now), now + 6);
  assert.equal(parseReset('1700000000', now), 1_700_000_000_000);
  const observations = parseRateLimitHeaders({ 'x-ratelimit-limit-requests': '1000', 'x-ratelimit-remaining-tokens-minute': '10', 'x-ratelimit-limit-tokens-minute': '60' }, { headerPeriods: { requests: 'day', tokens: 'minute' } }, now);
  assert.deepEqual(observations.map((item) => item.metric).sort(), ['rpd', 'tpm']);
});

test('budget reservations block concurrent overspend and settle actual cost', () => {
  db.prepare("INSERT INTO router_keys (id, user_id, name, prefix, key_hash, mode, budget_usd) VALUES ('k', 'u', 'k', 'p', 'h', 'paid', 1)").run();
  const key = db.prepare("SELECT * FROM router_keys WHERE id = 'k'").get();
  const hold = budget.reserve(key, 0.8);
  assert.ok(Math.abs(budget.remaining(key) - 0.2) < 1e-9);
  hold.settle(0.5, 'r1');
  assert.ok(Math.abs(budget.remaining(key) - 0.5) < 1e-9);
  assert.equal(budget.costOf({ isFree: false, priceIn: 1, priceOut: 2 }, { prompt_tokens: 1e6, completion_tokens: 5e5 }), 2);
  assert.equal(budget.costOf({ isFree: true, priceIn: 1, priceOut: 2 }, { prompt_tokens: 1e6, completion_tokens: 1e6 }), 0);
});

test('adaptive weights shift toward intelligence for complex requests only', () => {
  const weights = profiles.PRESETS.fast.weights;
  assert.deepEqual(scoring.adaptWeights(weights, 0.2, true), weights);
  const shifted = scoring.adaptWeights(weights, 1, true);
  assert.ok(shifted.intelligence > weights.intelligence);
  assert.ok(Math.abs(shifted.intelligence + shifted.speed + shifted.reliability + shifted.cost - 1) < 1e-9);
});

test('Gemini schema sanitizer keeps supported keys only', () => {
  const schema = sanitizeSchema({ $schema: 'x', type: 'object', additionalProperties: false, properties: { a: { type: ['string', 'null'], default: 'x' } }, required: ['a'] });
  assert.deepEqual(schema, { type: 'object', properties: { a: { type: 'string', nullable: true } }, required: ['a'] });
});

test('a zero free quota pauses the model for a day', () => {
  const model = entry();
  const now = Date.now();
  const error = classifyHttpError(429, { error: { message: 'You exceeded your current quota. Quota exceeded for metric: generate_content_free_tier_requests, limit: 0, model: gemini-pro' } });
  assert.equal(error.kind, KINDS.QUOTA_EXHAUSTED);
  learning.onFailure(model, error, { now });
  const snapshot = stats.snapshot(model.ref, now);
  assert.equal(snapshot.cooldown_reason, 'no_free_quota');
  assert.equal(snapshot.cooldown_until, now + 24 * 60 * 60 * 1000);
});

test('models that stay missing back off further each time', () => {
  const model = entry();
  const now = Date.now();
  const durations = [];
  for (let step = 0; step < 3; step += 1) {
    const at = now + step;
    learning.onFailure(model, new ProviderError(KINDS.NOT_FOUND, 'Function not found for account'), { now: at });
    durations.push(stats.snapshot(model.ref, at).cooldown_until - at);
  }
  assert.deepEqual(durations, [6 * 3600_000, 24 * 3600_000, 7 * 24 * 3600_000]);
});
