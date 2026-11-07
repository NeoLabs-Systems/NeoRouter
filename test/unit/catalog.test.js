'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const registry = require('../../server/providers/registry');
const { classify } = require('../../server/services/catalog/free_classifier');
const { judgeHeaders } = require('../../server/services/providers/billing_check');
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
  const groq = registry.get('groq');
  assert.equal(classify({ definition: groq, billingMode: 'free_tier', billingState: 'free', modelId: 'llama-3.1-8b-instant' }).reason, 'free_tier_account');
  assert.equal(classify({ definition: groq, billingMode: 'paid', billingState: 'free', modelId: 'llama-3.1-8b-instant' }).isFree, false);
  assert.equal(classify({ definition: registry.get('openai'), billingMode: 'free_tier', modelId: 'gpt-4o' }).isFree, false);
  // Credit trials and pay-per-token clouds are not free tiers.
  assert.equal(classify({ definition: registry.get('cerebras'), billingMode: 'free_tier', modelId: 'gpt-oss-120b' }).isFree, false);
  // No payment method exists on build.nvidia.com, so no verification is needed.
  assert.equal(classify({ definition: registry.get('nvidia'), billingMode: 'free_tier', modelId: 'meta/llama-3.3-70b-instruct' }).isFree, true);
});

test('providers that can bill a card need a verified free account', () => {
  const groq = registry.get('groq');
  const base = { definition: groq, billingMode: 'free_tier', modelId: 'openai/gpt-oss-20b' };
  assert.equal(classify({ ...base, billingState: 'unknown' }).reason, 'billing_unverified');
  assert.equal(classify({ ...base, billingState: 'paid' }).reason, 'billing_active');
  assert.equal(classify({ ...base, billingState: 'free' }).isFree, true);
});

test('partial free tiers cover only their listed models', () => {
  const google = { definition: registry.get('google'), billingMode: 'free_tier', billingState: 'free' };
  assert.equal(classify({ ...google, modelId: 'gemini-2.5-flash' }).isFree, true);
  assert.equal(classify({ ...google, modelId: 'gemma-4-31b-it' }).isFree, true);
  assert.equal(classify({ ...google, modelId: 'gemini-3.1-pro-preview' }).reason, 'not_in_free_tier');
  assert.equal(classify({ ...google, modelId: 'gemini-3.1-flash-image' }).isFree, false);
  assert.equal(classify({ ...google, modelId: 'gemini-4-argon' }).isFree, false);
  const ollama = { definition: registry.get('ollama_cloud'), billingMode: 'free_tier' };
  assert.equal(classify({ ...ollama, modelId: 'gpt-oss:120b' }).isFree, true);
  assert.equal(classify({ ...ollama, modelId: 'nemotron-3-super' }).isFree, true);
  assert.equal(classify({ ...ollama, modelId: 'kimi-k2.6' }).reason, 'not_in_free_tier');
  assert.equal(classify({ ...ollama, modelId: 'glm-5.3' }).isFree, false);
});

test('evidence from traffic beats every free rule except a manual override', () => {
  const openrouter = registry.get('openrouter');
  assert.equal(classify({ definition: openrouter, billingMode: 'paid', modelId: 'x:free', overrides: { billedAt: '2026-10-28' } }).reason, 'billed_observed');
  assert.equal(classify({ definition: openrouter, billingMode: 'paid', modelId: 'x:free', overrides: { billedAt: '2026-10-28', free: true } }).isFree, true);
  const google = { definition: registry.get('google'), billingMode: 'free_tier', billingState: 'free', modelId: 'gemini-2.5-pro' };
  assert.equal(classify({ ...google, overrides: { noFreeTier: true } }).reason, 'no_free_tier');
});

test('rate-limit headers tell free and paid Groq plans apart', () => {
  const verify = registry.get('groq').billing.verify;
  assert.equal(judgeHeaders(verify, { 'x-ratelimit-limit-requests': '14400' }).state, 'free');
  assert.equal(judgeHeaders(verify, { 'x-ratelimit-limit-requests': '1000' }).state, 'free');
  assert.equal(judgeHeaders(verify, { 'x-ratelimit-limit-requests': '500000' }).state, 'paid');
  assert.equal(judgeHeaders(verify, {}), null);
});

test('listing price zero counts only when the provider listing is trusted', () => {
  const zero = { freeHint: true, priceKnown: true };
  assert.equal(classify({ definition: registry.get('siliconflow'), billingMode: 'paid', modelId: 'x', listing: zero }).isFree, true);
  assert.equal(classify({ definition: registry.get('together'), billingMode: 'paid', modelId: 'x', listing: zero }).isFree, false);
  // Together needs a prepaid purchase before any use, so even "-Free" names are not free.
  assert.equal(classify({ definition: registry.get('together'), billingMode: 'paid', modelId: 'Llama-3.3-70B-Instruct-Turbo-Free', listing: zero }).isFree, false);
});

test('manual overrides win', () => {
  const definition = registry.get('openai');
  assert.equal(classify({ definition, billingMode: 'paid', modelId: 'x', overrides: { free: true } }).reason, 'manual_free');
  assert.equal(classify({ definition: registry.get('openrouter'), billingMode: 'paid', modelId: 'x:free', overrides: { free: false } }).isFree, false);
});

test('a connection-wide choice sets every model, but a model override and seen billing still win', () => {
  const definition = registry.get('openai');
  assert.equal(classify({ definition, billingMode: 'paid', modelId: 'x', accountOverride: 'free' }).reason, 'manual_free');
  const openrouter = registry.get('openrouter');
  assert.equal(classify({ definition: openrouter, billingMode: 'paid', modelId: 'x:free', accountOverride: 'paid' }).isFree, false, 'paid beats a free-looking model');
  assert.equal(classify({ definition, billingMode: 'paid', modelId: 'x', accountOverride: 'paid', overrides: { free: true } }).isFree, true, 'the model override is more specific');
  assert.equal(classify({ definition, billingMode: 'paid', modelId: 'x', accountOverride: 'free', overrides: { billedAt: Date.now() } }).isFree, false, 'real billing beats a blanket "free"');
  assert.equal(classify({ definition, billingMode: 'paid', modelId: 'x', accountOverride: 'free', listing: { paidOnly: true } }).isFree, false);
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
  assert.equal(normalizeModel({ id: 'whisper-large-v3' }).kind, 'transcription');
  assert.equal(normalizeModel({ id: 'gemini-3.5-transcribe' }).kind, 'transcription');
  assert.equal(normalizeModel({ id: 'canopylabs/orpheus-v1-english' }).kind, 'speech');
  assert.equal(normalizeModel({ id: 'gemini-3.8-flash-tts' }).kind, 'speech');
  assert.equal(normalizeModel({ id: 'black-forest-labs/flux.2-klein-4b' }).kind, 'image');
  assert.equal(normalizeModel({ id: 'gpt-realtime' }).kind, 'live');
  assert.equal(normalizeModel({ id: 'gemini-3.8-flash-live' }).kind, 'live');
  assert.equal(normalizeModel({ id: 'gemini-2.5-flash-native-audio-preview' }).kind, 'live');
  assert.equal(normalizeModel({ id: 'gpt-live-1' }).kind, 'live');
  assert.equal(normalizeModel({ id: 'olive-7b' }).kind, 'chat');
  assert.equal(normalizeModel({ id: 'lyria-2' }).kind, 'other');
  assert.equal(normalizeModel({ id: 'nvidia/nemotron-3.5-embed-1b', architecture: { output_modalities: ['embeddings'] } }).kind, 'embedding');
  assert.equal(normalizeModel({ id: 'x-ai/grok-imagine-video-1.5-lite', supported_durations: [4, 8] }).kind, 'video');
  assert.equal(normalizeModel({ id: 'prompt-guard-2-86m' }).kind, 'moderation');
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
  assert.ok(Math.abs(bench.intelligence - 1) < 1e-9, 'the best benchmarked model defines the top of the scale');
  const unknown = metadata.computeCanonical('nothingknown', ['nothing-known']);
  assert.equal(unknown.intelligence_source, 'default');
});

test('only the listed Z.ai flash models are free', () => {
  const definition = registry.get('zai');
  assert.equal(classify({ definition, billingMode: 'paid', modelId: 'glm-4.5-flash' }).isFree, true);
  assert.equal(classify({ definition, billingMode: 'paid', modelId: 'glm-5.3-flash' }).isFree, false);
  assert.equal(classify({ definition, billingMode: 'paid', modelId: 'glm-4.7-flashx' }).isFree, false);
});

test('two weak signals cannot produce a top intelligence score', () => {
  metadata.resetForTests({ modelsDev: { x: { 'mystery-v1': { priceIn: 0, priceOut: 0, releaseDate: new Date().toISOString().slice(0, 10), reasoning: true } } } });
  const record = metadata.computeCanonical(canonicalId('mystery-v1'), ['mystery-v1']);
  assert.ok(record.intelligence < 0.6, String(record.intelligence));
});

test('audited providers: exact free lists, local-only and keyless-only free tiers', () => {
  const zai = registry.get('zai');
  assert.equal(classify({ definition: zai, billingMode: 'paid', modelId: 'glm-4.7-flash' }).isFree, true);
  assert.equal(classify({ definition: zai, billingMode: 'paid', modelId: 'glm-5.3-flash' }).isFree, false);
  assert.equal(classify({ definition: registry.get('nous'), billingMode: 'free_tier', modelId: 'some/model' }).isFree, false);
  assert.equal(classify({ definition: registry.get('huggingface'), billingMode: 'free_tier', modelId: 'x' }).isFree, false);
  const ollama = registry.get('ollama');
  assert.equal(classify({ definition: ollama, billingMode: 'free_tier', modelId: 'llama3', baseUrl: 'http://192.168.1.5:11434/v1' }).isFree, true);
  assert.equal(classify({ definition: ollama, billingMode: 'free_tier', modelId: 'llama3', baseUrl: 'https://ollama.com/v1' }).isFree, false);
  const ovh = registry.get('ovh');
  assert.equal(classify({ definition: ovh, billingMode: 'free_tier', modelId: 'x' }).isFree, true);
  assert.equal(classify({ definition: ovh, billingMode: 'free_tier', modelId: 'x', hasSecret: true }).isFree, false);
  assert.equal(normalizeModel({ id: 'x', pricing: { prompt: '-1', completion: '-1' } }).freeHint, false);
  assert.equal(normalizeModel({ id: 'x', pricing: { prompt: '0', completion: '0' }, usage_based_only: true }).freeHint, false);
});

test('other routers are their own kind and dynamic prices are unknown, not cheap', () => {
  for (const id of ['openrouter/auto', 'kilo-auto/frontier', 'typesafe/jev-router', 'openrouter/free']) assert.equal(normalizeModel({ id }).kind, 'router', id);
  assert.equal(normalizeModel({ id: 'meta-llama/llama-3.3-70b-instruct' }).kind, 'chat');
  const dynamic = normalizeModel({ id: 'x', pricing: { prompt: '-1', completion: '-1' } });
  assert.equal(dynamic.priceKnown, false);
  assert.equal(dynamic.priceIn, null);
});

test('guesses are calibrated onto the benchmark scale', () => {
  const modelsDev = { a: {} };
  const artificialAnalysis = [];
  for (let i = 0; i < 16; i += 1) {
    const id = `family-${i}-${10 * (i + 1)}b`;
    modelsDev.a[id] = { priceIn: 0.1 * (i + 1), priceOut: 0.4 * (i + 1), releaseDate: '2026-01-01', reasoning: true };
    artificialAnalysis.push({ slug: id, name: id, intelligenceIndex: 15 + 2.5 * i });
  }
  modelsDev.a['unbenchmarked-400b'] = { priceIn: 1.6, priceOut: 6.4, releaseDate: '2026-01-01', reasoning: true };
  metadata.resetForTests({ modelsDev, artificialAnalysis });
  const guess = metadata.computeCanonical(canonicalId('unbenchmarked-400b'), ['unbenchmarked-400b']);
  const strongest = metadata.computeCanonical(canonicalId('family-15-160b'), ['family-15-160b']);
  assert.equal(guess.intelligence_source, 'calibrated');
  assert.equal(strongest.intelligence, 1);
  // Priced and sized like the strongest benchmarked models, so it lands near them, not above 1.
  assert.ok(guess.intelligence > 0.8 && guess.intelligence <= 0.9, String(guess.intelligence));
  metadata.resetForTests({});
});

test('benchmark names match provider ids without borrowing a sibling score', () => {
  metadata.resetForTests({
    artificialAnalysis: [
      { slug: 'nemotron-3-5-super-vl', name: 'Nemotron 3.5 Super VL', intelligenceIndex: 14 },
      { slug: 'reflection-beam', name: 'Beam 1.0', intelligenceIndex: 18 },
      { slug: 'deepseek-v4-1-flash-1017', name: 'DeepSeek V4.1 Flash 1017 (Max)', intelligenceIndex: 32 },
      { slug: 'glm-5-3', name: 'GLM-5.3 (Max)', intelligenceIndex: 31 },
      { slug: 'glm-5-3-low', name: 'GLM-5.3 (Low)', intelligenceIndex: 23 },
      { slug: 'qwen3-8-max', name: 'Qwen3.8 Max', intelligenceIndex: 34 },
      { slug: 'qwen3-8-27b', name: 'Qwen3.8 27B (Xhigh)', intelligenceIndex: 20 },
      { slug: 'llama-4-70b', name: 'Llama 4 70B', intelligenceIndex: 25 },
      { slug: 'llama-4-8b', name: 'Llama 4 8B', intelligenceIndex: 10 },
    ],
  });
  const index = (id) => {
    const record = metadata.computeCanonical(canonicalId(id), [id]);
    return record.intelligence_source === 'artificial_analysis' ? Math.round(record.intelligence * 34) : null;
  };
  assert.equal(index('nvidia/nemotron-3.5-super-vl-120b-a12b'), 14);
  assert.equal(index('reflection/beam-1.0-501b-a23b:free'), 18);
  assert.equal(index('deepseek-ai/deepseek-v4.1-flash'), 32);
  assert.equal(index('z-ai/glm-5.3'), 31, 'the default effort wins over "low"');
  assert.equal(index('z-ai/glm-5.3-flash'), null, 'a "flash" variant is a different model');
  assert.equal(index('qwen/qwen3.8-27b'), 20);
  assert.equal(index('meta/llama-4-8b-instruct'), 10);
  assert.equal(index('meta/llama-4'), null, 'sizes that collapse onto one name stay ambiguous');
  metadata.resetForTests({});
});

test('Artificial Analysis records keep their individual benchmark scores', () => {
  const { toRecord } = require('../../server/services/catalog/metadata/artificial_analysis');
  const record = toRecord({ slug: 'old-model', name: 'Old', evaluations: { artificial_analysis_intelligence_index: null, gpqa: 0.7, hle: 0.1, mmlu_pro: null } });
  assert.equal(record.intelligenceIndex, null);
  assert.deepEqual(record.benchmarks, { gpqa: 0.7, hle: 0.1 });
});

test('providers with free models offer "Free", and a free connection skips models that need credits', () => {
  require('../../server/db/migrate').migrate();
  const { rejection } = require('../../server/services/routing/router');
  for (const id of ['nous', 'kilo', 'anyapi']) assert.equal(registry.publicDefinition(registry.get(id)).billing.freeTierAvailable, true, id);
  const nous = registry.get('nous');
  assert.deepEqual(classify({ definition: nous, billingMode: 'free_tier', modelId: 'hermes-5:free' }).isFree, true);
  const paid = classify({ definition: nous, billingMode: 'free_tier', modelId: 'hermes-5-405b', listing: { priceKnown: true } });
  assert.equal(paid.isFree, false);
  const entry = (free, billingMode, reason) => ({
    enabled: true, available: true, accountEnabled: true, accountStatus: 'healthy', isFree: free, isPlan: false, billingMode, freeReason: reason,
    id: 'nous/x', providerSlug: 'nous', definitionId: 'nous', stats: {}, intelligence: 0.5,
  });
  const ctx = { key: { mode: 'both', allow_free: 1, allow_plan: 1, allowed_models_json: '[]' }, needs: {}, inputTokens: 10, request: { messages: [] }, constraints: {}, freeOnly: false };
  assert.equal(rejection(entry(false, 'free_tier', paid.reason), ctx, Date.now()), 'needs_credits');
  assert.notEqual(rejection(entry(false, 'paid', paid.reason), ctx, Date.now()), 'needs_credits', 'a paid connection still uses it');
});

test('Google live models are free only once the account is confirmed to have no billing', () => {
  const { classify } = require('../../server/services/catalog/free_classifier');
  const definition = registry.get('google');
  const judge = (modelId, billingState) => classify({ definition, billingMode: 'free_tier', billingState, modelId });
  for (const modelId of ['gemini-3.8-live', 'gemini-3.1-flash-live-preview', 'gemini-2.5-flash-native-audio-latest', 'gemini-3.5-transcribe-live', 'gemini-3.5-live-translate-preview']) {
    assert.equal(judge(modelId, 'free').isFree, true, modelId);
    assert.deepEqual(judge(modelId, 'paid'), { isFree: false, reason: 'billing_active' }, modelId);
    assert.deepEqual(judge(modelId, 'unknown'), { isFree: false, reason: 'billing_unverified' }, modelId);
  }
  assert.equal(judge('gemini-robotics-er-2-streaming-preview', 'free').isFree, false);
  assert.equal(judge('gemini-3.1-pro-preview', 'free').isFree, false);
});

test('OpenRouter benchmarks: the newest version and the middle host speak for a model', () => {
  const { toModels } = require('../../server/services/catalog/metadata/openrouter_benchmarks');
  const row = (permaslug, provider, accuracy, questions = 200, at = '2026-11-01') => ({ benchmark: 'gpqa_diamond', model_slug: 'lab/model-a', model_name: 'Model A', model_permaslug: permaslug, provider_name: provider, accuracy, total_questions: questions, last_run_timestamp: at });
  const [model, ...rest] = toModels([
    row('lab/model-a-old', 'Old', 0.2, 200, '2026-09-01'),
    row('lab/model-a-new', 'Good', 0.9),
    row('lab/model-a-new', 'Fine', 0.88),
    row('lab/model-a-new', 'Broken', 0.01),
    row('lab/model-a-new', 'Too few', 1, 5),
    { ...row('lab/model-a-new', 'Zero', 0), benchmark: 'unknown' },
  ]);
  assert.equal(rest.length, 0);
  assert.equal(model.benchmarks.gpqa, 0.88);
  assert.equal(model.benchmarks.tau, undefined);
});

test('the OpenRouter snapshot of the day is used, falling back to the one before', async () => {
  const { fetchSource } = require('../../server/services/catalog/metadata/openrouter_benchmarks');
  const now = Date.parse('2026-11-04T01:00:00Z');
  const asked = [];
  const original = global.fetch;
  global.fetch = async (url) => {
    asked.push(url.split('/').pop());
    if (url.endsWith('2026-11-04.json')) return { ok: false, status: 404 };
    return { ok: true, json: async () => ({ snapshot_date: '2026-11-03', rows: [{ benchmark: 'gpqa_diamond', model_slug: 'a/b', model_permaslug: 'a/b-1', accuracy: 0.5, total_questions: 100, last_run_timestamp: '2026-11-01' }] }) };
  };
  try {
    const result = await fetchSource({ now });
    assert.deepEqual(asked, ['2026-11-04.json', '2026-11-03.json']);
    assert.equal(result.snapshotDate, '2026-11-03');
    const previous = { snapshotDate: '2026-11-04', models: [] };
    assert.equal(await fetchSource({ now, previous }), previous, 'a dated file never changes');
    assert.equal(asked.length, 2);
  } finally {
    global.fetch = original;
  }
});

test('OpenRouter scores work with no Artificial Analysis key, and a model only it knows counts as measured', () => {
  metadata.resetForTests({ openrouterBenchmarks: { snapshotDate: '2026-11-04', models: [
    { slug: 'lab/strong-1', name: 'Strong', benchmarks: { gpqa: 0.95, tau: 0.8 } },
    { slug: 'lab/weak-1', name: 'Weak', benchmarks: { gpqa: 0.475, tau: 0.4 } },
  ] } });
  const strong = metadata.computeCanonical('strong1', ['lab/strong-1'], 'chat');
  const weak = metadata.computeCanonical('weak1', ['lab/weak-1'], 'chat');
  assert.equal(strong.intelligence_source, 'openrouter_benchmarks');
  assert.equal(strong.intelligence, 1);
  assert.equal(weak.intelligence, 0.5);
  assert.deepEqual(JSON.parse(weak.skills_json), { reasoning: 0.5, agentic: 0.5 });
  metadata.resetForTests({});
});

test('a benchmark that ranks models unlike the fuller index gets no say', () => {
  const names = Array.from({ length: 12 }, (_, i) => `lab/model-${i}`);
  metadata.resetForTests({
    artificialAnalysis: names.map((slug, i) => ({ slug: slug.split('/')[1], name: slug, intelligenceIndex: 10 + i * 5, benchmarks: {} })),
    openrouterBenchmarks: { snapshotDate: '2026-11-04', models: names.map((slug, i) => ({ slug, name: slug, benchmarks: { gpqa: 0.3 + i * 0.05, tau: 0.5 + ((i * 7) % 12) * 0.01 } })) },
  });
  const agreement = metadata.openRouterAgreement();
  assert.ok(agreement.gpqa > 0.9);
  const skills = JSON.parse(metadata.computeCanonical('model11', ['lab/model-11'], 'chat').skills_json);
  assert.ok(skills.reasoning > 0.9);
  if (agreement.tau !== undefined && agreement.tau < 0.3) assert.equal(skills.agentic, undefined);
  metadata.resetForTests({});
});

test('Artificial Analysis: a Free key reads every page of the /free list', async () => {
  const aa = require('../../server/services/catalog/metadata/artificial_analysis');
  aa.resetForTests();
  const asked = [];
  const original = global.fetch;
  const pages = { 1: { data: [{ id: 'a', slug: 'one' }], pagination: { page: 1, total_pages: 2, has_more: true } }, 2: { data: [{ id: 'b', slug: 'two' }], pagination: { page: 2, total_pages: 2, has_more: false } } };
  global.fetch = async (url, options) => {
    asked.push(url.replace('https://artificialanalysis.ai/api/v2/', ''));
    assert.equal(options.headers['x-api-key'], 'test-key');
    if (url.endsWith('/language/models')) return { ok: false, status: 403 };
    return { ok: true, status: 200, json: async () => pages[new URL(url).searchParams.get('page') || 1] };
  };
  try {
    const items = await aa.fetchList('language/models', { apiKey: 'test-key' });
    assert.deepEqual(items.map((item) => item.slug), ['one', 'two']);
    assert.deepEqual(asked, ['language/models', 'language/models/free', 'language/models/free?page=2']);
    asked.length = 0;
    await aa.fetchList('media/text-to-image/models', { apiKey: 'test-key' });
    assert.equal(asked[0], 'media/text-to-image/models/free', 'the tier is remembered, saving a request');
  } finally {
    global.fetch = original;
    aa.resetForTests();
  }
});

test('Artificial Analysis: a failed page keeps the list we already have', async () => {
  const aa = require('../../server/services/catalog/metadata/artificial_analysis');
  aa.resetForTests();
  const original = global.fetch;
  let call = 0;
  global.fetch = async () => {
    call += 1;
    return call === 1 ? { ok: true, status: 200, json: async () => ({ data: [{ slug: 'one' }], pagination: { has_more: true } }) } : { ok: false, status: 429 };
  };
  try {
    await assert.rejects(aa.fetchSource({ apiKey: 'k' }), /page 2 .*429|429/);
  } finally {
    global.fetch = original;
    aa.resetForTests();
  }
});

test('Artificial Analysis: records read the current response shape', () => {
  const { toRecord } = require('../../server/services/catalog/metadata/artificial_analysis');
  const record = toRecord({
    id: 'id-1', slug: 'claude-sonnet-5-5-medium', name: 'Claude Sonnet 5.5', release_date: '2026-09-28',
    evaluations: { artificial_analysis_intelligence_index: 30.8, artificial_analysis_coding_index: null },
    performance: { median_output_tokens_per_second: 88.82, median_time_to_first_token_seconds: 1.47 },
  });
  assert.deepEqual(record, { id: 'id-1', slug: 'claude-sonnet-5-5-medium', name: 'Claude Sonnet 5.5', intelligenceIndex: 30.8, benchmarks: {}, outputTps: 88.82, ttftSeconds: 1.47, releaseDate: '2026-09-28' });
});

test('Artificial Analysis: the Pro names of benchmarks map onto the ones skills use', () => {
  const { toRecord } = require('../../server/services/catalog/metadata/artificial_analysis');
  const record = toRecord({ slug: 'm', name: 'M', evaluations: { gpqa_diamond: 0.917, aa_lcr: 0.797, terminalbench_v4_0: 0.419, hle: 0.423, scicode: null, critpt: 0.191 } });
  assert.deepEqual(record.benchmarks, { gpqa: 0.917, lcr: 0.797, terminalbench_hard: 0.419, hle: 0.423 });
});

test('Artificial Analysis: benchmark scores the Free list lacks are kept, old index values are not', () => {
  const { mergeKnown } = require('../../server/services/catalog/metadata/artificial_analysis');
  const merged = mergeKnown(
    [{ id: 'x', slug: 'kept', name: 'Kept', intelligenceIndex: 30, benchmarks: {} }],
    [
      { slug: 'kept', name: 'Kept', intelligenceIndex: 70, benchmarks: { gpqa: 0.9 } },
      { slug: 'dropped', name: 'Dropped', intelligenceIndex: 60, indexDerived: true, benchmarks: { hle: 0.2 } },
      { slug: 'nothing', name: 'Nothing', intelligenceIndex: 50, benchmarks: {} },
    ],
  );
  assert.deepEqual(merged.map((record) => record.slug), ['kept', 'dropped']);
  assert.equal(merged[0].intelligenceIndex, 30);
  assert.deepEqual(merged[0].benchmarks, { gpqa: 0.9 });
  assert.equal(merged[1].intelligenceIndex, null, 'on the old scale, so it is estimated again');
  assert.deepEqual(merged[1].benchmarks, { hle: 0.2 });
});

test('Artificial Analysis media lists use the new paths', async () => {
  const aa = require('../../server/services/catalog/metadata/artificial_analysis');
  const media = require('../../server/services/catalog/metadata/artificial_analysis_media');
  aa.resetForTests();
  const asked = [];
  const original = global.fetch;
  global.fetch = async (url) => {
    asked.push(url.replace('https://artificialanalysis.ai/api/v2/', ''));
    if (!url.endsWith('/free')) return { ok: false, status: 403 };
    return { ok: true, status: 200, json: async () => ({ data: [{ slug: 'm', name: 'M', elo: 1200, model_creator: { name: 'Lab' } }, { slug: 'no-elo', name: 'N', elo: null }] }) };
  };
  try {
    const result = await media.fetchSource({ apiKey: 'k' });
    assert.deepEqual(asked, ['media/text-to-image/models', 'media/text-to-image/models/free', 'media/text-to-speech/models/free', 'media/text-to-video/models/free']);
    assert.deepEqual(result.image, [{ slug: 'm', name: 'M', creator: 'Lab', elo: 1200, releaseDate: null }]);
    assert.ok(!asked.some((path) => path.includes('data/')), 'no legacy path');
  } finally {
    global.fetch = original;
    aa.resetForTests();
  }
});
