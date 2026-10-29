'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const quota = require('../../server/services/quota/quota_service');
const plans = require('../../server/services/routing/plans');
const definitions = require('../../server/providers/definitions/plans');

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
  assert.equal(plans.factor(entry), 1.1, 'room left: the plan is preferred');
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
