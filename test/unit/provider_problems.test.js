'use strict';

// Refusals seen on a real server, read by what they mean.
require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const { classifyHttpError, announcedReturn, KINDS } = require('../../server/providers/provider_error');

test('Cloudflare’s error list is read as a message, and its free-plan refusal needs a person', () => {
  const error = classifyHttpError(403, { errors: [{ message: 'AiError: Model @cf/zai-org/glm-5.3 is not available on the Workers Free plan' }] });
  assert.equal(error.message, 'AiError: Model @cf/zai-org/glm-5.3 is not available on the Workers Free plan');
  assert.equal(error.kind, KINDS.NOT_FOUND);
  assert.equal(error.needsAction, true);
});

test('consents, phone verification and data-policy objections need a person, not retries', () => {
  for (const message of [
    'gemma-4:free needs your consent for these providers: rtw. Give it once at https://unorouter.com/consents',
    'To prevent abuse and high traffic on free models, phone verification is required.',
    'You objected to passing your inputs to providers that use them for their own purposes',
  ]) {
    const error = classifyHttpError(403, { error: { message } });
    assert.equal(error.kind, KINDS.NOT_FOUND, message);
    assert.equal(error.needsAction, true, message);
  }
  assert.equal(classifyHttpError(401, { error: { message: 'Invalid API key' } }).needsAction, false);
});

test('a trial’s monthly cap is a used-up quota, not a rate limit', () => {
  const error = classifyHttpError(429, { message: 'You are using a Trial key, which is limited to 1000 API calls / month.' });
  assert.equal(error.kind, KINDS.QUOTA_EXHAUSTED);
});

test('a planned outage that says when it ends waits until then', () => {
  const now = Date.UTC(2026, 10, 7, 9, 30);
  assert.equal(announcedReturn('the Spark is switching models; back in about 11 min.', now), 11 * 60_000);
  assert.equal(announcedReturn('scheduled training session; inference is back at 12:00 UTC.', now), 150 * 60_000);
  assert.equal(announcedReturn('back at 08:00 UTC', now), 22.5 * 60 * 60_000, 'a time already past today means tomorrow');
  assert.equal(announcedReturn('upstream exploded', now), null);
  const error = classifyHttpError(503, { error: { message: 'inference is back in 5 min' } });
  assert.equal(error.kind, KINDS.SERVER);
  assert.equal(error.retryAfterMs, 5 * 60_000);
});

test('ElevenLabs’ nested detail message is read', () => {
  const error = classifyHttpError(402, { detail: { type: 'payment_required', message: 'Free users cannot use library voices via the API.' } });
  assert.equal(error.kind, KINDS.PAYMENT);
  assert.equal(error.message, 'Free users cannot use library voices via the API.');
});

test('learning: needs-action and announced outages get their own cooldowns', () => {
  const learning = require('../../server/services/routing/learning');
  const row = { cooldown_level: 0 };
  const now = Date.now();
  const action = learning.cooldownFor(classifyHttpError(403, { error: { message: 'phone verification is required' } }), row, now);
  assert.equal(action.reason, 'needs_action');
  assert.ok(action.until - now >= 86_400_000);
  const downtime = learning.cooldownFor(classifyHttpError(503, { error: { message: 'back in about 10 min' } }), row, now);
  assert.equal(downtime.reason, 'scheduled_downtime');
  assert.equal(downtime.until, now + 10 * 60_000);
  assert.equal(learning.cooldownFor(classifyHttpError(503, { error: { message: 'boom' } }), row, now), null, 'an outage without an end keeps the breaker in charge');
});

test('a search plan used up is a used-up allowance with a readable message, not a broken connection', async () => {
  const { classifyHttpError, KINDS: kinds } = require('../../server/providers/provider_error');
  // Tavily: HTTP 432 and { detail: { error } }.
  const tavily = classifyHttpError(432, { detail: { error: "This request exceeds your plan's set usage limit. Please upgrade your plan or contact support@tavily.com" } }, new Headers());
  assert.equal(tavily.kind, kinds.QUOTA_EXHAUSTED);
  assert.match(tavily.message, /^This request exceeds your plan's set usage limit/);
  // Brave: 429 and { type, error: { detail, meta } }.
  const brave = classifyHttpError(429, { type: 'ErrorResponse', error: { id: 'x', status: 429, detail: 'Request quota limit exceeded for plan.', meta: { plan: 'Free', quota_limit: 2000, quota_current: 2001 } } }, new Headers());
  assert.equal(brave.kind, kinds.QUOTA_EXHAUSTED);
  assert.equal(brave.message, 'Request quota limit exceeded for plan.');

  // The health check keeps such a connection healthy.
  const { SearchAdapter } = require('../../server/providers/adapters/search_engines');
  for (const error of [tavily, brave]) {
    const adapter = Object.create(SearchAdapter.prototype);
    adapter.search = async () => { throw error; };
    assert.deepEqual(await adapter.validate(), { status: 'healthy', message: null });
  }
  // A refused key still is a problem.
  const refused = Object.create(SearchAdapter.prototype);
  refused.search = async () => { throw classifyHttpError(401, { detail: { error: 'Unauthorized: missing or invalid API key.' } }, new Headers()); };
  assert.equal((await refused.validate()).status, 'invalid');
});
