// Generates the JS oracle fixtures the Go tests compare against.
//
//   node internal/providers/testdata/gen_fixtures.mjs
//
// Every case is run through the JS implementation it ports — provider_error.js,
// model_info.js, session_id.js and the definition modules' quota probe parsers —
// and written to testdata/js-fixtures.json. The Go tests replay the inputs and
// require the same answers, so a behaviour change on either side fails a test
// instead of quietly diverging.

import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import fs from 'node:fs';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, '..', '..', '..');
const require = createRequire(import.meta.url);

const errors = require(path.join(root, 'server', 'providers', 'provider_error'));
const modelInfo = require(path.join(root, 'server', 'providers', 'model_info'));
const sessionId = require(path.join(root, 'server', 'providers', 'session_id'));
const plans = require(path.join(root, 'server', 'providers', 'definitions', 'plans'));

// ---------------------------------------------------------------- errors

const errorBodies = [
  // Cloudflare: an error list.
  { errors: [{ message: 'AiError: Model @cf/zai-org/glm-5.3 is not available on the Workers Free plan' }] },
  // Consent, phone verification, data-policy objection: a person is needed.
  { error: { message: 'gemma-4:free needs your consent for these providers: rtw. Give it once at https://unorouter.com/consents' } },
  { error: { message: 'To prevent abuse and high traffic on free models, phone verification is required.' } },
  { error: { message: 'You objected to passing your inputs to providers that use them for their own purposes' } },
  { error: { message: 'Invalid API key' } },
  { error: { message: 'model agreement not signed' } },
  // A trial's monthly cap is a used-up quota, not a rate limit.
  { message: 'You are using a Trial key, which is limited to 1000 API calls / month.' },
  // ElevenLabs: a nested detail message.
  { detail: { type: 'payment_required', message: 'Free users cannot use library voices via the API.' } },
  // Tavily (432) and Brave (429) plan limits.
  { detail: { error: "This request exceeds your plan's set usage limit. Please upgrade your plan or contact support@tavily.com" } },
  { type: 'ErrorResponse', error: { id: 'x', status: 429, detail: 'Request quota limit exceeded for plan.', meta: { plan: 'Free', quota_limit: 2000, quota_current: 2001 } } },
  // Ollama Cloud's included usage used up.
  { error: 'Add usage credits or subscribe and save up to 66%: https://ollama.com/plans (ref: eae75f6f)' },
  { error: 'Insufficient balance' },
  // Rate limits with a learnable limit in the message.
  { error: { message: 'Rate limit reached for requests: Limit 500, Used 500. Please try again in 20ms.' } },
  { error: { message: 'Limit: 0, this model has no free quota.' } },
  { error: { message: 'You have exceeded the rate limit of 200000 tokens per minute.' } },
  { error: { message: 'Daily limit of 1000 requests reached.' } },
  // Context length.
  { error: { message: "This model's maximum context length is 8192 tokens, however you requested 9000 tokens." } },
  // Region and plan refusals reachable only from a 403.
  { error: { message: 'This model is not available in your region.' } },
  { error: { message: 'Upgrade your plan to use this model.' } },
  { error: { message: 'terms acceptance is required for this model' } },
  { error: { message: 'an active subscription is required' } },
  // Bodies whose error member is not a string: the whole body is the message.
  { error: { code: 'invalid_api_key', param: null } },
  { detail: { error: { reason: 'nested object' } } },
  { errors: [{ code: 400 }] },
  { error: { detail: { nested: true } } },
  // Bodies that are not shaped at all.
  'plain text refusal',
  { error: 'string error' },
  { detail: { error: 'nested string error' } },
  null,
];

const statuses = [400, 401, 402, 403, 404, 408, 413, 422, 429, 432, 433, 500, 503, 529];

const classify = [];
for (const status of statuses) {
  for (const body of errorBodies) {
    const error = errors.classifyHttpError(status, body);
    classify.push({
      status,
      body,
      headers: {},
      expect: {
        kind: error.kind,
        message: error.message,
        retryAfterMs: error.retryAfterMs,
        needsAction: error.needsAction,
        learned: error.learned,
      },
    });
  }
}

// Retry-after parsing, in both spellings, and the 500 branch that reads an
// announced return out of the message.
for (const headers of [{ 'retry-after': '2' }, { 'retry-after': '0' }, { 'retry-after-ms': '1500' }, { 'retry-after': 'abc' }]) {
  const error = errors.classifyHttpError(429, { error: { message: 'Too many requests' } }, new Headers(headers));
  classify.push({
    status: 429,
    body: { error: { message: 'Too many requests' } },
    headers,
    expect: { kind: error.kind, message: error.message, retryAfterMs: error.retryAfterMs, needsAction: error.needsAction, learned: error.learned },
  });
}

const announced = [
  { message: 'the Spark is switching models; back in about 11 min.', now: Date.UTC(2026, 10, 7, 9, 30) },
  { message: 'scheduled training session; inference is back at 12:00 UTC.', now: Date.UTC(2026, 10, 7, 9, 30) },
  { message: 'back at 08:00 UTC', now: Date.UTC(2026, 10, 7, 9, 30) },
  { message: 'back in 5 min', now: Date.UTC(2026, 10, 7, 9, 30) },
  { message: 'back in ~3 min', now: Date.UTC(2026, 10, 7, 9, 30) },
  { message: 'upstream exploded', now: Date.UTC(2026, 10, 7, 9, 30) },
].map((item) => ({ ...item, expect: errors.announcedReturn(item.message, item.now) }));

const announcedClassified = errors.classifyHttpError(503, { error: { message: 'inference is back in 5 min' } });
classify.push({
  status: 503,
  body: { error: { message: 'inference is back in 5 min' } },
  headers: {},
  expect: {
    kind: announcedClassified.kind,
    message: announcedClassified.message,
    retryAfterMs: announcedClassified.retryAfterMs,
    needsAction: announcedClassified.needsAction,
    learned: announcedClassified.learned,
  },
});

const learned = [
  'rate_limit: concurrency limit: 4 in-flight requests',
  'Limit 1000, used 1000',
  'limit: 0',
  'You have exceeded the rate limit of 200000 tokens per minute.',
  'tokens per day: limit 500000',
  'no limit here',
].map((message) => ({ message, expect: errors.learnFromMessage(message) }));

const extracted = errorBodies.map((body) => ({ body, expect: errors.extractMessage(body) }));

const allowance = [
  'Add usage credits or subscribe and save up to 66%',
  'session usage limit reached',
  'daily usage limit',
  'insufficient balance',
].map((message) => ({ message, expect: errors.allowanceUsedUp(message) }));

// ---------------------------------------------------------------- models

const modelCases = [
  { raw: { id: 'gpt-4o', name: 'GPT-4o', context_length: 128000, pricing: { prompt: '0.0000025', completion: '0.00001' } } },
  { raw: { id: 'claude-sonnet', display_name: 'Claude Sonnet', top_provider: { context_length: 200000, max_completion_tokens: 8192 }, pricing: { prompt: '0.000003', completion: '0.000015', input_cache_read: '0.0000003' } } },
  { raw: { id: 'some-model:free', pricing: { prompt: '0.000001', completion: '0.000002' } } },
  { raw: { id: 'freebie', pricing: 'free' } },
  { raw: { id: 'paid-only', pricing: { prompt: '0.000001', completion: '0.000002' }, paid_only: true } },
  { raw: { id: 'dynamic', pricing: { prompt: '-1', completion: '-1' } } },
  { raw: { id: 'requesty-model', input_price: '0.000002', output_price: '0.000004' } },
  { raw: { id: 'listed-cost', cost: { text: { input: { price: 0.15 }, output: { price: 0.6 } } } } },
  { raw: { id: 'per-token-unit', pricing: { unit: 'token', prompt: 0.000002, completion: 0.000004 } } },
  { raw: { id: 'media-video', pricing_skus: { duration_seconds_720p: 0.04, duration_seconds_1080p: 0.09, cents_per_video_output_second_480p: 2 } } },
  { raw: { id: 'media-image', price_per_image: 0.04 } },
  { raw: { id: 'media-call', price_per_call: 0.001, price_per_search: 0.005 } },
  { raw: { id: 'with-params', supported_parameters: ['tools', 'response_format', 'reasoning', 'structured_outputs'], context_length: 32000 } },
  { raw: { id: 'with-capabilities', capabilities: { tools: { supported: false }, vision: true, reasoning: { supported: true } } } },
  { raw: { id: 'with-modalities', architecture: { input_modalities: ['text', 'image/png'], output_modalities: ['text'] } } },
  { raw: { id: 'video-out', architecture: { output_modalities: ['video'] } } },
  { raw: { id: 'decision-out', architecture: { output_modalities: ['decisions'] } } },
  { raw: { id: 'audio-out', architecture: { output_modalities: ['speech'] } } },
  { raw: { id: 'declared-type', type: 'embedding' } },
  { raw: { id: 'declared-img', category: 'image' } },
  { raw: { id: 'declared-search', task: 'search' } },
  { raw: { id: 'declared-weather', type: 'weather' } },
  { raw: { id: 'declared-live', type: 'live' } },
  { raw: { id: 'durations', supported_durations: [4, 8] } },
  { raw: { id: 'limit-object', limit: { context: 64000, output: 4096 } } },
  { raw: { id: 'kebab-context', max_model_len: 1000000, max_output_tokens: 8192 } },
  { raw: { name: 'only-a-name' } },
  { raw: { id: 'priced-with-cache', pricing: { prompt: 0.5, completion: 1.5, input_cache_read: 0.1, input_cache_write: 0.2 } } },
  { raw: { id: 'string-price', pricing: { prompt: '$1,200', completion: ' 3 ' } } },
  { raw: { id: 'empty-price', pricing: { prompt: '', completion: null } } },
  // Kinds, by name.
  ...['text-embedding-3-large', 'bge-m3', 'rerank-english', 'whisper-large-v3', 'parakeet-tdt', 'gpt-4o-mini-tts', 'orpheus-v1', 'gpt-live', 'gemini-live-2', 'realtime-preview', 'lyria-002', 'video-edit-1', 'aleph-v2', 'dall-e-3', 'stable-diffusion-xl', 'flux-pro', 'diffusiongemma-9b', 'veo-3', 'sora-2', 'wan2-1', 'kling-2', 'hailuo-01', 'openrouter/auto', 'openrouter/free', 'kilo-auto/frontier', 'jev-router', 'llama-guard-4', 'omni-moderation-latest', 'prompt-guard-2', 'deep-research-pro', 'computer-use-preview', 'gpt-4o-ocr', 'fuyu-8b', 'gpt-4.1', 'nano-banana', 'phoenix-1.0'].map((id) => ({ raw: { id } })),
  // Overrides an adapter applies.
  { raw: { id: 'codex-model' }, overrides: { tools: true, vision: true, reasoning: true } },
  { raw: { id: 'cloudflare-embed', context_length: 512 }, overrides: { kind: 'embedding', contextWindow: 512, tools: false, vision: false } },
  { raw: { id: 'local-model', pricing: { prompt: 1, completion: 2 } }, overrides: { kind: 'chat', priceKnown: true, priceIn: 0, priceOut: 0, freeHint: true } },
  { raw: { id: 'speech-model' }, overrides: { kind: 'speech' } },
];

const normalize = modelCases.map((item) => {
  const record = modelInfo.normalizeModel(JSON.parse(JSON.stringify(item.raw)), item.overrides || {});
  return { raw: item.raw, overrides: item.overrides || {}, expect: JSON.parse(JSON.stringify(record)) };
});

// ---------------------------------------------------------------- sessions

const sessionCases = [
  { canonical: { messages: [{ role: 'system', content: 'You are helpful.' }, { role: 'user', content: 'Hello there' }] }, salt: '' },
  { canonical: { messages: [{ role: 'user', content: [{ type: 'text', text: 'Part one. ' }, { type: 'text', text: 'Part two.' }] }] }, salt: '' },
  { canonical: { messages: [{ role: 'user', content: 'Hi' }] }, salt: 'account-1' },
  { canonical: { messages: [] }, salt: '' },
  { canonical: { messages: [{ role: 'developer', content: 'dev' }, { role: 'assistant', content: 'assistant first' }, { role: 'user', content: 'q' }] }, salt: 'x' },
].map((item) => ({ ...item, expect: sessionId.derivedSession(item.canonical, item.salt) }));

// ---------------------------------------------------------------- quota probes

const quotaCases = [];
const record = (parser, body, run) => quotaCases.push({ parser, body, expect: JSON.parse(JSON.stringify(run(body) ?? [])) });

// definition id -> name of the Go port of its quota parser (gen_definitions.mjs
// writes the same names into the definition data).
const quotaParsers = {
  openrouter: 'openrouter_credits',
  deepseek: 'deepseek_balance',
  siliconflow: 'siliconflow_balance',
  synthetic: 'synthetic_plan',
  minimax_plan: 'minimax_windows',
  opencode_go: 'opencode_go_windows',
};

const definitions = require(path.join(root, 'server', 'providers', 'registry'));
for (const definition of definitions.list()) {
  const parse = definition.quota?.probe?.parse;
  if (typeof parse !== 'function') continue;
  const name = quotaParsers[definition.id];
  if (!name) {
    console.error(`No Go parser name for ${definition.id}; add it to quotaParsers.`);
    process.exit(1);
  }
  const samples = {
    openrouter: [
      { data: { limit: 20, limit_remaining: 12.5, usage: 7.5, rate_limit: { requests: 20 } } },
      { data: { limit: 10, usage: 4 } },
      { data: { limit: null } },
      { data: {} },
      { data: { limit: 5, limit_remaining: 'nonsense' } },
    ],
    deepseek: [
      { balance_infos: [{ currency: 'USD', total_balance: '12.34' }, { currency: 'CNY', total_balance: '88' }] },
      { balance_infos: [] },
      {},
    ],
    siliconflow: [{ data: { totalBalance: '9.5' } }, { data: { balance: '1.25' } }, { data: {} }],
    synthetic: [
      { subscription: { limit: 100, requests: 30, renewsAt: '2026-11-08T00:00:00Z' } },
      { subscription: { limit: 100, requests: 300 } },
      { subscription: { limit: 'x' } },
      {},
    ],
    minimax_plan: [
      { model_remains: [
        { model_name: 'MiniMax-M3', current_interval_remaining_percent: 62, current_weekly_remaining_percent: 81, end_time: 1762500000, weekly_end_time: 1762900000 },
        { model_name: 'MiniMax-M3-highspeed', current_interval_remaining_percent: 20, current_weekly_remaining_percent: 55, end_time: 1762500100, weekly_end_time: 1762900100 },
        { model_name: 'hailuo-video', current_interval_remaining_percent: 1, current_weekly_remaining_percent: 1 },
      ] },
      { model_remains: [{ model_name: 'MiniMax-M3' }] },
      {},
    ],
    opencode_go: [
      { usage: { rolling: { percent: 40, resetsAt: '2026-11-08T00:00:00Z' }, weekly: { usagePercent: 12 }, monthly: { percent: 99 } } },
      { rolling: { percent: 5 } },
      { usage: {} },
      {},
    ],
  };
  for (const body of samples[definition.id] || []) record(name, body, parse);
}
// plans.js exports its two parsers; the registry data names them.
record('minimax_windows', { model_remains: [{ model_name: 'M3', current_interval_remaining_percent: 30, end_time: 1762500000 }] }, plans.minimaxWindows);
record('opencode_go_windows', { usage: { rolling: { percent: 10 }, monthly: { percent: 50, resetsAt: '2026-11-08T00:00:00Z' } } }, plans.opencodeGoWindows);

// ------------------------------------------------- public definitions

// The connect screen's view of each provider. The slot waits come from the
// configuration, so the fixture names the values it used.
const waits = { slotWaitMs: 45000, slotMaxWaitMs: 300000 };
const publicDefinitions = definitions.list().map((definition) => ({
  id: definition.id,
  expect: JSON.parse(JSON.stringify(definitions.publicDefinition(definition))),
}));

const fixtures = {
  classify,
  announcedReturn: announced,
  learnFromMessage: learned,
  extractMessage: extracted,
  allowanceUsedUp: allowance,
  normalizeModel: normalize,
  derivedSession: sessionCases,
  quota: quotaCases,
  waits,
  publicDefinition: publicDefinitions,
};

const text = `${JSON.stringify(fixtures, null, 2)}\n`;
const target = path.join(here, 'js-fixtures.json');
fs.writeFileSync(target, text);
console.log(`wrote ${path.relative(root, target)}: ${classify.length} classify, ${normalize.length} models, ${quotaCases.length} quota, ${sessionCases.length} sessions (${Buffer.byteLength(text)} bytes)`);