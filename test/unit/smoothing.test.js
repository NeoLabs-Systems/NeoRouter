'use strict';

// Even out usage: allowances are spent at a steady pace once there is enough
// data, high-priority keys and paid models are left alone.
require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const { migrate } = require('../../server/db/migrate');
const { getDatabase } = require('../../server/db/database');
const quota = require('../../server/services/quota/quota_service');
const smoothing = require('../../server/services/routing/smoothing');

const HOUR = 3_600_000;
const DAY = 24 * HOUR;

test.before(() => {
  migrate(getDatabase(), { quiet: true });
  const db = getDatabase();
  db.prepare("INSERT INTO users (id, username, email, password_hash, role) VALUES ('u1', 'one', 'one@example.test', 'x', 'user')").run();
  db.prepare("INSERT INTO users (id, username, email, password_hash, role) VALUES ('u2', 'two', 'two@example.test', 'x', 'user')").run();
});

test.beforeEach(() => {
  quota.resetForTests();
  smoothing.resetForTests();
});

const free = (ref) => ({ ref, accountId: `acc-${ref}`, modelId: 'some-model', definitionId: 'custom_openai', isFree: true, isPlan: false });

test('not ready without a week of history, enough requests and known limits', () => {
  const now = Date.now();
  const empty = smoothing.readiness('u1', now);
  assert.equal(empty.ready, false);
  assert.deepEqual([empty.days, empty.activeDays, empty.requests], [0, 0, 0]);
  assert.throws(() => smoothing.setEnabled('u1', true, now), (error) => error.code === 'NOT_ENOUGH_DATA');

  // Nine days of answers, but to models without a known limit: still not ready.
  const insert = getDatabase().prepare("INSERT INTO requests (id, user_id, created_at, api_format, requested_model, status) VALUES (?, 'u1', ?, 'chat', 'auto', 'success')");
  for (let day = 0; day < 9; day += 1) for (let i = 0; i < 15; i += 1) insert.run(`r-${day}-${i}`, now - day * DAY - i * 60_000 - HOUR);
  smoothing.resetForTests();
  const unknownLimits = smoothing.readiness('u1', now);
  assert.equal(unknownLimits.days >= 8, true);
  assert.equal(unknownLimits.activeDays >= 8, true);
  assert.equal(unknownLimits.requests, 135);
  assert.equal(unknownLimits.coverage, 0);
  assert.equal(unknownLimits.ready, false, 'limits must be known for the traffic');
});

test('known limits are counted over free traffic only; a per-minute limit counts as known', () => {
  const db = getDatabase();
  const now = Date.now();
  db.prepare("INSERT INTO users (id, username, email, password_hash, role) VALUES ('u3', 'three', 'three@example.test', 'x', 'user')").run();
  db.prepare(`INSERT INTO provider_accounts (id, user_id, definition_id, slug, label, billing_mode, enabled, status)
    VALUES ('acc3', 'u3', 'custom_openai', 'mine', 'Mine', 'free_tier', 1, 'healthy')`).run();
  const model = db.prepare("INSERT INTO models (account_id, model_id, canonical_id, capability, is_free, enabled, available) VALUES ('acc3', ?, ?, 'chat', ?, 1, 1)");
  const refs = {};
  for (const [name, free] of [['daily', 1], ['minute', 1], ['unknown', 1], ['paid', 0]]) refs[name] = Number(model.run(`model-${name}`, `model-${name}`, free).lastInsertRowid);
  quota.setLimit(`m:${refs.daily}`, 'rpd', 1000, 'manual');
  quota.setLimit(`m:${refs.minute}`, 'rpm', 30, 'manual');
  const insert = db.prepare("INSERT INTO requests (id, user_id, created_at, api_format, requested_model, model_ref, status) VALUES (?, 'u3', ?, 'chat', 'auto', ?, 'success')");
  // 30 daily-limited, 30 per-minute only, 20 unknown, 200 paid (left out).
  const counts = { daily: 30, minute: 30, unknown: 20, paid: 200 };
  for (const [name, count] of Object.entries(counts)) for (let i = 0; i < count; i += 1) insert.run(`${name}-${i}`, now - (i % 6) * DAY - HOUR, refs[name]);
  const readiness = smoothing.readiness('u3', now);
  assert.equal(readiness.coverage, 0.75, '60 of the 80 free answers have known limits; paid ones do not count');
});

test('a daily limit is spread over the day: an hour gets its share, with room for peaks', () => {
  const now = Date.now();
  const entry = free('m1');
  quota.setLimit('m:m1', 'rpd', 240, 'manual');
  // No history: every hour counts the same, 240 / 24 × 1.6 ≈ 16 requests an hour.
  assert.equal(smoothing.held(entry, { userId: 'u2', now }), false);
  for (let i = 0; i < 15; i += 1) quota.recordRequest(entry, 100, now - i * 60_000);
  assert.equal(smoothing.held(entry, { userId: 'u2', now }), false, '15 of about 16 is fine');
  quota.recordRequest(entry, 100, now);
  assert.equal(smoothing.held(entry, { userId: 'u2', now }), true, 'past its share for this hour');
  assert.equal(smoothing.held(entry, { userId: 'u2', priority: 'high', now }), false, 'a high-priority key still gets through');
  assert.equal(smoothing.held({ ...entry, isFree: false }, { userId: 'u2', now }), false, 'paid models are not paced');
  // An hour later the window has moved on.
  assert.equal(smoothing.held(entry, { userId: 'u2', now: now + HOUR + 60_000 }), false);
});

test('a plan may run only a little ahead of the time that has passed', () => {
  const now = Date.now();
  const plan = { ...free('p1'), isFree: false, isPlan: true };
  // A week with six days to go: 1/7 of it has passed.
  quota.setState('a:acc-p1', 'plan_week', { limit: 100, remaining: 90, resetAt: now + 6 * DAY }, 'headers', now);
  assert.equal(smoothing.held(plan, { userId: 'u2', now }), false, '10 % used at 14 % of the week');
  quota.setState('a:acc-p1', 'plan_week', { limit: 100, remaining: 60, resetAt: now + 6 * DAY }, 'headers', now);
  assert.equal(smoothing.held(plan, { userId: 'u2', now }), true, '40 % used at 14 % of the week');
  assert.equal(smoothing.held(plan, { userId: 'u2', now: now + 4 * DAY }), false, 'by day five, 40 % is behind schedule');
});

test('a plan window whose reset time has passed is shown renewed', () => {
  const now = Date.now();
  quota.setState('a:acc-shown', 'plan_week', { limit: 100, remaining: 7, resetAt: now - 3600_000 }, 'headers', now - 2 * DAY);
  quota.setState('a:acc-shown', 'plan_5h', { limit: 100, remaining: 99, resetAt: now + 3600_000 }, 'headers', now);
  const shown = Object.fromEntries(quota.accountStateNow('acc-shown', now).map((state) => [state.metric, state]));
  assert.equal(shown.plan_week.remaining, 100, 'full again');
  assert.equal(shown.plan_week.resetAt, null, 'the next reset is not known yet');
  assert.equal(shown.plan_week.renewed, true);
  assert.equal(shown.plan_5h.remaining, 99, 'a running window stays as reported');
  assert.equal(shown.plan_5h.renewed, undefined);
});
