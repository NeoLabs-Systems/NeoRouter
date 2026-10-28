'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const registry = require('../../server/providers/registry');
const { classify } = require('../../server/services/catalog/free_classifier');
const { canonicalId, parameterCount } = require('../../server/services/catalog/model_identity');
const { normalizeModel } = require('../../server/providers/model_info');
const metadata = require('../../server/services/catalog/metadata_service');

test('every provider definition is valid and ids are unique', () => {
  const definitions = registry.list();
  assert.ok(definitions.length > 60);
  for (const definition of definitions) {
    assert.ok(definition.baseUrl || definition.editableBaseUrl, `${definition.id} needs a base URL`);
    for (const pattern of definition.billing.alwaysFree) assert.doesNotThrow(() => new RegExp(pattern));
  }
});

test('unknown price is never free', () => {
  const definition = registry.get('openai');
  const result = classify({ definition, billingMode: 'paid', modelId: 'gpt-x', listing: { priceKnown: false } });
  assert.deepEqual(result, { isFree: false, reason: 'price_unknown' });
});

test('OpenRouter :free models are free even on a paid account', () => {
  const definition = registry.get('openrouter');
  assert.equal(classify({ definition, billingMode: 'paid', modelId: 'meta-llama/llama-3.3-70b-instruct:free' }).isFree, true);
  assert.equal(classify({ definition, billingMode: 'paid', modelId: 'openai/gpt-4o', listing: { priceKnown: true } }).isFree, false);
});

test('free-tier accounts are free only on providers whose free tier cannot charge', () => {
  assert.equal(classify({ definition: registry.get('groq'), billingMode: 'free_tier', modelId: 'llama-3.1-8b-instant' }).reason, 'free_tier_account');
  assert.equal(classify({ definition: registry.get('groq'), billingMode: 'paid', modelId: 'llama-3.1-8b-instant' }).isFree, false);
  assert.equal(classify({ definition: registry.get('openai'), billingMode: 'free_tier', modelId: 'gpt-4o' }).isFree, false);
});

test('listing price zero counts only when the provider listing is trusted', () => {
  const zero = { freeHint: true, priceKnown: true };
  assert.equal(classify({ definition: registry.get('siliconflow'), billingMode: 'paid', modelId: 'x', listing: zero }).isFree, true);
  assert.equal(classify({ definition: registry.get('together'), billingMode: 'paid', modelId: 'x', listing: zero }).isFree, false);
  assert.equal(classify({ definition: registry.get('together'), billingMode: 'paid', modelId: 'Llama-3.3-70B-Instruct-Turbo-Free', listing: zero }).isFree, true);
});

test('manual overrides win', () => {
  const definition = registry.get('openai');
  assert.equal(classify({ definition, billingMode: 'paid', modelId: 'x', overrides: { free: true } }).reason, 'manual_free');
  assert.equal(classify({ definition: registry.get('openrouter'), billingMode: 'paid', modelId: 'x:free', overrides: { free: false } }).isFree, false);
});

test('pricing parser handles per-token and per-million prices', () => {
  const openrouter = normalizeModel({ id: 'a/b', pricing: { prompt: '0.0000005', completion: '0.0000015' } });
  assert.ok(Math.abs(openrouter.priceIn - 0.5) < 1e-9);
  assert.ok(Math.abs(openrouter.priceOut - 1.5) < 1e-9);
  const together = normalizeModel({ id: 'x', pricing: { input: 0.88, output: 0.88 } });
  assert.equal(together.priceIn, 0.88);
  const free = normalizeModel({ id: 'x', pricing: { prompt: '0', completion: '0', request: '0' } });
  assert.equal(free.freeHint, true);
  const partial = normalizeModel({ id: 'x', pricing: { prompt: '0' } });
  assert.equal(partial.priceKnown, false);
});

test('model kinds separate chat from embeddings, audio and images', () => {
  assert.equal(normalizeModel({ id: 'text-embedding-3-small' }).kind, 'embedding');
  assert.equal(normalizeModel({ id: 'whisper-large-v3' }).kind, 'audio');
  assert.equal(normalizeModel({ id: 'llama-guard-4-12b' }).kind, 'moderation');
  assert.equal(normalizeModel({ id: 'llama-3.3-70b-versatile' }).kind, 'chat');
  assert.equal(normalizeModel({ id: 'nvidia/riva-translate-4b-instruct-v2' }).kind, 'other');
});

test('canonical ids unify the same model across hosts', () => {
  assert.equal(canonicalId('llama-3.3-70b-versatile'), canonicalId('meta-llama/llama-3.3-70b-instruct:free'));
  assert.equal(canonicalId('@cf/meta/llama-3.3-70b-instruct-fp8-fast'), canonicalId('llama3.3:70b'));
  assert.equal(canonicalId('claude-3-5-sonnet-20241022'), canonicalId('claude-3.5-sonnet'));
  assert.equal(canonicalId('gpt-4o-2024-08-06'), 'gpt4o');
  assert.notEqual(canonicalId('gpt-4o'), canonicalId('gpt-4o-mini'));
  assert.equal(parameterCount('mixtral-8x7b'), 56);
});

test('intelligence uses benchmark data when present and heuristics otherwise', () => {
  metadata.resetForTests({
    modelsDev: {
      a: { 'big-model-405b': { priceIn: 3, priceOut: 15, contextWindow: 128000, reasoning: true, releaseDate: '2025-06-01' } },
      b: { 'tiny-model-1b': { priceIn: 0.02, priceOut: 0.04, contextWindow: 8000, reasoning: false, releaseDate: '2023-03-01' } },
    },
    artificialAnalysis: [{ slug: 'bench-model', name: 'Bench Model', intelligenceIndex: 60 }],
  });
  const big = metadata.computeCanonical(canonicalId('big-model-405b'), ['big-model-405b']);
  const tiny = metadata.computeCanonical(canonicalId('tiny-model-1b'), ['tiny-model-1b']);
  const bench = metadata.computeCanonical(canonicalId('bench-model'), ['bench-model']);
  assert.ok(big.intelligence > tiny.intelligence + 0.3, `${big.intelligence} vs ${tiny.intelligence}`);
  assert.equal(big.intelligence_source, 'heuristic');
  assert.equal(bench.intelligence_source, 'artificial_analysis');
  assert.ok(Math.abs(bench.intelligence - 0.8) < 1e-9);
  const unknown = metadata.computeCanonical('nothingknown', ['nothing-known']);
  assert.equal(unknown.intelligence_source, 'default');
});

test('Z.ai flash models are free, flashx is not', () => {
  const definition = registry.get('zai');
  assert.equal(classify({ definition, billingMode: 'paid', modelId: 'glm-5.3-flash' }).isFree, true);
  assert.equal(classify({ definition, billingMode: 'paid', modelId: 'glm-5.3-flashx' }).isFree, false);
});

test('two weak signals cannot produce a top intelligence score', () => {
  metadata.resetForTests({ modelsDev: { x: { 'mystery-v1': { priceIn: 0, priceOut: 0, releaseDate: new Date().toISOString().slice(0, 10), reasoning: true } } } });
  const record = metadata.computeCanonical(canonicalId('mystery-v1'), ['mystery-v1']);
  assert.ok(record.intelligence < 0.6, String(record.intelligence));
});
