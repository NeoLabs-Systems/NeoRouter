'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const quota = require('../../server/services/quota/quota_service');
const plans = require('../../server/services/routing/plans');
const definitions = require('../../server/providers/definitions/plans');
const scoring = require('../../server/services/routing/scoring');

const synthetic = definitions.find((item) => item.id === 'synthetic');

test.before(() => require('../../server/db/migrate').migrate());

test('Synthetic and MiniMax quota answers become plan windows', () => {
  assert.deepEqual(synthetic.quota.probe.parse({ subscription: { limit: 135, requests: 35, renewsAt: '2026-11-01T00:00:00.000Z' } }),
    [{ metric: 'plan_requests', limit: 135, remaining: 100, resetAt: Date.parse('2026-11-01T00:00:00.000Z') }]);
  const minimax = definitions.minimaxWindows({ model_remains: [
    { model_name: 'MiniMax-M3.1-Flash', current_interval_remaining_percent: 40, end_time: 1000, current_weekly_remaining_percent: 70, weekly_end_time: 2000 },
    { model_name: 'Hailuo-video', current_interval_remaining_percent: 0, current_weekly_remaining_percent: 0 },
  ] });
  assert.deepEqual(minimax, [
    { metric: 'plan_5h', limit: 100, remaining: 40, resetAt: 1000 },
    { metric: 'plan_week', limit: 100, remaining: 70, resetAt: 2000 },
  ], 'video models outside the plan do not count as used up');
});

test('the tightest window decides, and requests since the probe count down', () => {
  quota.resetForTests();
  plans.resetForTests();
  const now = Date.now();
  const entry = { accountId: 'acc-plan', isPlan: true, planOverflow: false };
  quota.setState('a:acc-plan', 'plan_requests', { limit: 10, remaining: 2, resetAt: now + 3_600_000 }, 'probe', now);
  quota.setState('a:acc-plan', 'plan_week', { limit: 100, remaining: 90, resetAt: now + 86_400_000 }, 'probe', now);
  assert.equal(plans.room(entry).share, 0.2);
  assert.ok(plans.factor(entry) > 1, 'well ahead of schedule: the plan is preferred');
  plans.onSent(entry);
  assert.equal(plans.factor(entry), 0.5, 'running low: held back');
  plans.onSent(entry);
  assert.equal(plans.rejection(entry), 'plan_used_up');
  // "Plan, then pay per use" turns into a normally billed model instead.
  const overflow = { ...entry, planOverflow: true };
  assert.equal(plans.effective(overflow).isPlan, false);
  // After the reset time the window is full again.
  assert.equal(plans.room(entry, now + 2 * 3_600_000).share, 0.9);
});

test('the allowance costs more the faster it goes, and nothing when it would expire unused', () => {
  quota.resetForTests();
  const now = Date.now();
  const entry = { accountId: 'acc-paced', isPlan: true, planOverflow: false };
  assert.equal(plans.pace(entry, now), null);
  assert.equal(plans.allowanceCost(entry, now), 0.08, 'unknown pace: a little, so good-enough free models win ties');
  // 44 % of a 5-hour window left with 3 hours to go: used faster than it renews.
  quota.setState('a:acc-paced', 'plan_5h', { limit: 100, remaining: 44, resetAt: now + 3 * 3_600_000 }, 'headers', now);
  assert.ok(Math.abs(plans.pace(entry, now) - 0.44 / 0.6) < 1e-9);
  const behind = plans.allowanceCost(entry, now);
  assert.ok(behind > 0.08 && behind < 0.2, String(behind));
  assert.equal(plans.factor(entry, now), 1);
  assert.equal(scoring.cost(entry, now), 1 - behind, 'routing sees it as the plan model\'s price');
  // 90 % left with 20 minutes to go: most of it would be lost.
  quota.setState('a:acc-paced', 'plan_5h', { limit: 100, remaining: 90, resetAt: now + 20 * 60_000 }, 'headers', now);
  assert.equal(plans.allowanceCost(entry, now), 0);
  assert.ok(plans.factor(entry, now) > 1);
  // The tighter window decides: a weekly allowance far behind holds the plan back.
  quota.setState('a:acc-paced', 'plan_week', { limit: 100, remaining: 10, resetAt: now + 6 * 86_400_000 }, 'headers', now);
  assert.ok(plans.allowanceCost(entry, now) > behind);
  assert.equal(plans.allowanceCost({ ...entry, isPlan: false }, now), 0);
});
