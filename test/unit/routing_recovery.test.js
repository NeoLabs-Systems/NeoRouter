'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const { classifyHttpError, KINDS } = require('../../server/providers/provider_error');
const { detectKind } = require('../../server/providers/model_info');
const accountHealth = require('../../server/services/routing/account_health');
const capacity = require('../../server/services/routing/capacity');
const retries = require('../../server/services/routing/task/retries');
const scoring = require('../../server/services/routing/scoring');
const metadata = require('../../server/services/catalog/metadata_service');

test('a model or plan the account may not use is not a rejected key', () => {
  for (const message of [
    'AiError: Model @cf/zai-org/glm-5 is not available on the Workers Free plan',
    'AiError: Model Agreement: Prior to using this model, you must submit the prompt',
    'AiError: Ai: This account is not allowed to access @cf/swiss-ai/apertus',
    'swe-1-6-slow:free needs your consent for these providers',
    'To prevent abuse and high traffic on free models, phone verification is required.',
  ]) assert.equal(classifyHttpError(403, { message }).kind, KINDS.NOT_FOUND, message);
  assert.equal(classifyHttpError(401, { message: 'Invalid API key' }).kind, KINDS.AUTH);
});

test('a connection that only fails rests, longer each time, until one success', () => {
  const now = 1_000_000;
  for (let i = 0; i < accountHealth.FAILURES_TO_REST - 1; i++) accountHealth.onFailure('a', now);
  assert.equal(accountHealth.restingUntil('a', now), null);
  accountHealth.onFailure('a', now);
  assert.equal(accountHealth.restingUntil('a', now), now + accountHealth.FIRST_REST_MS);
  const later = now + accountHealth.FIRST_REST_MS + 1;
  assert.equal(accountHealth.restingUntil('a', later), null, 'then it gets another try');
  for (let i = 0; i < 3; i++) accountHealth.onFailure('a', later);
  assert.equal(accountHealth.restingUntil('a', later), later + 2 * accountHealth.FIRST_REST_MS, 'a few failures send it back, for longer');
  accountHealth.onSuccess('a');
  assert.equal(accountHealth.restingUntil('a', later), null);
});

test('short of capacity, a request is told sooner unless its key has high priority', () => {
  const key = { user_id: 'u1', priority: 'normal' };
  assert.equal(capacity.queueWaitMs(key, 45_000), 45_000);
  for (let i = 0; i < capacity.MIN_SAMPLES; i++) capacity.record('u1', i % 2 === 0);
  assert.equal(capacity.isShort('u1'), true);
  assert.equal(capacity.queueWaitMs(key, 45_000), capacity.SHORT_WAIT_MS);
  assert.equal(capacity.queueWaitMs({ ...key, priority: 'high' }, 45_000), 45_000);
  for (let i = 0; i < 200; i++) capacity.record('u1', true);
  assert.equal(capacity.isShort('u1'), false, 'and it recovers');
});

test('the same conversation again soon after an answer counts against that answer, once', () => {
  const id = retries.fingerprint('key', [{ role: 'user', content: 'hi' }]);
  assert.notEqual(id, retries.fingerprint('key', [{ role: 'user', content: 'other' }]));
  assert.equal(retries.repeatedAnswer(id, 1000), null);
  retries.rememberAnswer(id, { ref: 5, task: 'coding' }, 1000);
  assert.equal(retries.repeatedAnswer(id, 2000).ref, 5);
  assert.equal(retries.repeatedAnswer(id, 2000), null);
  retries.rememberAnswer(id, { ref: 5, task: 'coding' }, 1000);
  assert.equal(retries.repeatedAnswer(id, 1000 + 4 * 60 * 1000), null, 'much later it is simply a new question');
});

test('a score nobody measured cannot outrank a measured one', () => {
  const guess = scoring.trustedIntelligence({ intelligence: 0.5, intelligenceConfidence: 0, intelligenceSource: 'default' });
  const measured = scoring.trustedIntelligence({ intelligence: 0.29, intelligenceConfidence: 0.9, intelligenceSource: 'artificial_analysis' });
  assert.equal(measured, 0.29);
  assert.ok(guess < measured, `${guess} vs ${measured}`);
  assert.equal(scoring.trustedIntelligence({ intelligence: 0.9, intelligenceConfidence: 0, intelligenceSource: 'manual' }), 0.9, 'set by hand stands');
});

test('models that do not chat are not listed as chat models', () => {
  for (const id of ['voyage-4', 'voyage-code-4']) assert.equal(detectKind(id), 'embedding', id);
  for (const id of ['nai-diffusion-3', 'pony-diffusion-v6-xl', 'flat-2d-animerge:free', 'lucid-origin', 'phoenix-1.0']) assert.equal(detectKind(id), 'image', id);
  assert.equal(detectKind('Qwen3Guard-Gen-8B'), 'moderation');
  for (const id of ['google/diffusiongemma-26b-a4b-it', 'phoenix', 'gemma-4-31b']) assert.equal(detectKind(id), 'chat', id);
});

test('benchmarks are not borrowed across model sizes', () => {
  const record = { slug: 'hermes-3-llama-3-1-70b', name: 'Hermes 3 70B' };
  assert.equal(metadata.sameSize('hermes-3-llama-3.1-405b', record), false);
  assert.equal(metadata.sameSize('hermes-3-llama-3.1-70b', record), true);
  assert.equal(metadata.sameSize('hermes-3', record), true);
  assert.equal(metadata.sameSize('beam-1.0-501b-a23b', { slug: 'beam', name: 'Beam' }), true);
});

test('an index far from the model\'s own benchmark scores is met halfway', () => {
  assert.equal(metadata.reconcileWithSkills(0.5, { coding: 0.55, reasoning: 0.5 }), 0.5);
  assert.ok(Math.abs(metadata.reconcileWithSkills(0.51, { coding: 0.67, reasoning: 0.73 }) - 0.605) < 0.001);
  assert.equal(metadata.reconcileWithSkills(0.5, null), 0.5);
});
