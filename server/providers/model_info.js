'use strict';

// Normalizes the many shapes of `/models` entries into one record. Prices are
// USD per 1M tokens. `priceKnown` is only true when the provider itself
// reported a price; `freeHint` is only true when every reported price field is
// exactly zero or the provider labels the model free. Unknown never means free.

const NON_CHAT_PATTERNS = [
  ['embedding', /embed|bge-|e5-|gte-|nomic-embed|text-similarity|(^|\/)voyage-/i],
  ['rerank', /rerank/i],
  // Audio is split by direction: speech-to-text and text-to-speech.
  ['transcription', /whisper|transcri|(^|[-_/])asr([-_]|$)|parakeet|canary-/i],
  ['speech', /tts|text-to-speech|orpheus|playai|melotts|kokoro|voice-design/i],
  // Speech-to-speech sessions over a WebSocket (GPT-Live, Realtime, Gemini Live).
  ['live', /(^|[-_/.])live([-_/.]|$)|realtime|native-audio/i],
  ['other', /lyria|music|audio/i],
  // Editing models need an input video, which /v1/videos does not take.
  ['other', /video-(edit|upscale)|(^|\/)aleph|detector/i],
  // "diffusion" alone also names text models such as diffusiongemma.
  ['image', /dall-e|diffusion(?!gemma)|sdxl|flux|imagen|image-gen|gpt-image|kolors|seedream|nano-banana|-image(-|$)|animerge|lucid-origin|(^|\/)phoenix-\d/i],
  ['video', /\bvideo\b|veo-|sora|wan2|(^|[/-])kling|hailuo/i],
  // Other routers ("openrouter/auto", "kilo-auto/frontier", Jev Router) pick a
  // model themselves at an unpredictable price; NeoRouter does the routing.
  ['router', /(^|\/)auto(-beta)?$|(^|\/)kilo-auto\/|jev-router|(^|\/)openrouter\/free$|(^|\/)router$/i],
  ['moderation', /moderation|llama-guard|shieldgemma|prompt-guard|safeguard|content-safety|nemoguard|safety-guard|topic-control|qwen[\d.]*guard/i],
  // Agents, tool-only and legacy multimodal endpoints that do not accept plain chat.
  ['other', /deep-research|computer-use|robotics|antigravity|deplot|kosmos|fuyu|calibration|reward|embedqa|parse|translat|\bocr\b|-ocr|retriever/i],
];

function detectKind(id, raw = {}) {
  // `category` is Pollinations' kind field (text, image, video, embedding, audio, …).
  const declared = String(raw.type || raw.object_type || raw.task || raw.category || '').toLowerCase();
  if (/embed/.test(declared)) return 'embedding';
  if (declared === 'video') return 'video';
  if (declared === 'search') return 'search';
  if (declared === 'weather') return 'weather';
  if (declared === 'live') return 'live';
  if (/image/.test(declared) && !/text/.test(declared)) return 'image';
  if (raw.supported_durations || raw.supported_resolutions) return 'video';
  const outputs = raw.architecture?.output_modalities;
  if (Array.isArray(outputs) && outputs.length && !outputs.includes('text')) {
    if (outputs.includes('embeddings') || outputs.includes('embedding')) return 'embedding';
    if (outputs.includes('video')) return 'video';
    // Decision models answer typed questions: a System One kind.
    if (outputs.includes('decisions')) return 'moderation';
    // OpenRouter names text-to-speech output "speech", other listings "audio".
    return outputs.includes('image') ? 'image' : outputs.includes('audio') || outputs.includes('speech') ? 'speech' : 'other';
  }
  for (const [kind, pattern] of NON_CHAT_PATTERNS) {
    if (pattern.test(id)) return kind;
  }
  return 'chat';
}

function toNumber(value) {
  if (value === null || value === undefined || value === '') return null;
  const number = typeof value === 'number' ? value : Number(String(value).replace(/[$,\s]/g, ''));
  return Number.isFinite(number) ? number : null;
}

// OpenRouter-style pricing is USD per token; most others per 1M tokens. A
// per-token price is always tiny, so values below 0.01 are treated as such.
function perMillion(value, unitHint) {
  if (value === null) return null;
  if (unitHint === 'token') return value * 1e6;
  if (unitHint === 'million') return value;
  return value !== 0 && value < 0.01 ? value * 1e6 : value;
}

// Listings in the models.dev / OpenCode style: `cost: { text: { input: { price },
// output: { price } } }`, always in USD per million tokens (an explicit unit, so
// a small price is not mistaken for a per-token one). Cache and long-context
// prices are not read: the base price is the one a typical request pays.
function parseListedCost(cost) {
  const text = cost && typeof cost === 'object' ? cost.text : null;
  if (!text || typeof text !== 'object') return null;
  const input = toNumber(text.input?.price ?? text.input);
  const output = toNumber(text.output?.price ?? text.output);
  if (input === null || output === null || input < 0 || output < 0) return null;
  return { priceIn: input, priceOut: output, priceKnown: true, freeHint: input === 0 && output === 0 };
}

// The listing says this model needs paid credit whatever the plan or price
// (Pollinations' paid_only, usage_based_only, NavyAI's required_plan).
function isPaidOnly(raw) {
  return raw.paid_only === true || raw.usage_based_only === true || Boolean(raw.required_plan);
}

function parsePricing(raw) {
  if (isPaidOnly(raw)) {
    return { priceIn: null, priceOut: null, priceKnown: false, freeHint: false };
  }
  // Free markers in a listing: xKiro access_tier, Api.Airforce and KiosAPI
  // tier, BlockRun billing_mode, Blaze and Iteracompute is_free.
  if (raw.price === 'free' || raw.pricing === 'free' || raw.is_free === true || raw.free === true
    || raw.access_tier === 'free' || raw.tier === 'free' || raw.billing_mode === 'free') {
    return { priceIn: 0, priceOut: 0, priceKnown: true, freeHint: true };
  }
  // Requesty: input_price / output_price per token at the top level (its
  // pricing field is a list of context tiers).
  const topIn = toNumber(raw.input_price);
  const topOut = toNumber(raw.output_price);
  if (topIn !== null && topOut !== null && topIn >= 0 && topOut >= 0) {
    return { priceIn: perMillion(topIn), priceOut: perMillion(topOut), priceKnown: true, freeHint: topIn === 0 && topOut === 0 };
  }
  const pricing = raw.pricing && typeof raw.pricing === 'object' && !Array.isArray(raw.pricing) ? raw.pricing : null;
  const priced = pricing ? null : parseListedCost(raw.cost);
  if (priced) return priced;
  if (pricing) {
    const values = Object.values(pricing).map(toNumber).filter((value) => value !== null);
    const unit = pricing.unit === 'token' ? 'token' : undefined;
    const prompt = toNumber(pricing.prompt ?? pricing.input ?? pricing.input_tokens);
    const completion = toNumber(pricing.completion ?? pricing.output ?? pricing.output_tokens);
    // A negative price ("-1") marks a dynamic price that is only known after
    // the call: treat it as unknown, never as cheap.
    if (values.some((value) => value < 0)) return { priceIn: null, priceOut: null, priceKnown: false, freeHint: false };
    if (values.length && prompt !== null && completion !== null) {
      const allZero = values.every((value) => value === 0);
      return {
        priceIn: perMillion(prompt, unit),
        priceOut: perMillion(completion, unit),
        priceKnown: true,
        freeHint: allZero,
      };
    }
  }
  return { priceIn: null, priceOut: null, priceKnown: false, freeHint: false };
}

// Unit prices for media listings, in USD: OpenRouter's video SKUs are cents
// per output second per resolution; the cheapest resolution is the floor.
function parseUnitPricing(raw) {
  const skus = raw.pricing_skus && typeof raw.pricing_skus === 'object' ? raw.pricing_skus : null;
  if (skus) {
    // Either "cents_per_video_output_second_720p" (cents) or "duration_seconds_720p" (USD).
    const seconds = Object.entries(skus)
      .filter(([name]) => /second/.test(name) && !/reference|input/.test(name))
      .map(([name, value]) => (toNumber(value) ?? -1) / (/^cents/.test(name) ? 100 : 1))
      .filter((value) => value >= 0);
    if (seconds.length) return { priceUnit: 'second', unitPrice: Math.min(...seconds) };
  }
  const perMinute = toNumber(raw.price_per_minute);
  if (perMinute !== null && perMinute >= 0) return { priceUnit: 'minute', unitPrice: perMinute };
  const perMchar = toNumber(raw.price_per_mchar);
  if (perMchar !== null && perMchar >= 0) return { priceUnit: 'mchar', unitPrice: perMchar };
  const perSearch = toNumber(raw.price_per_search);
  if (perSearch !== null && perSearch >= 0) return { priceUnit: 'search', unitPrice: perSearch };
  const perCall = toNumber(raw.price_per_call);
  if (perCall !== null && perCall >= 0) return { priceUnit: 'call', unitPrice: perCall };
  const perImage = toNumber(raw.price_per_image ?? raw.pricing?.per_image);
  if (perImage !== null && perImage >= 0) return { priceUnit: 'image', unitPrice: perImage };
  return { priceUnit: null, unitPrice: null };
}

function supportedParameters(raw) {
  return Array.isArray(raw.supported_parameters) ? raw.supported_parameters : null;
}

function normalizeModel(raw, overrides = {}) {
  const id = String(raw.id || raw.name || raw.model || '').trim();
  const params = supportedParameters(raw);
  const inputs = raw.architecture?.input_modalities || raw.input_modalities || raw.modalities?.input;
  const contextWindow = toNumber(raw.context_length ?? raw.context_window ?? raw.max_context_length
    ?? raw.top_provider?.context_length ?? raw.max_model_len ?? raw.inputTokenLimit ?? raw.limit?.context);
  const maxOutput = toNumber(raw.max_output_tokens ?? raw.top_provider?.max_completion_tokens ?? raw.max_completion_tokens
    ?? raw.outputTokenLimit ?? raw.limit?.output);
  const pricing = parsePricing(raw);
  if (/:free$/.test(id)) {
    pricing.freeHint = true;
    pricing.priceKnown = true;
    pricing.priceIn = 0;
    pricing.priceOut = 0;
  }
  const flag = (value) => (value === undefined || value === null ? null : Boolean(value));
  // A capability can be a plain flag or an object such as { supported: false }.
  const supports = (value) => (value && typeof value === 'object' && 'supported' in value ? value.supported : value);
  return {
    id,
    name: raw.display_name || raw.displayName || raw.name || id,
    kind: detectKind(id, raw),
    contextWindow: contextWindow && contextWindow > 0 ? Math.round(contextWindow) : null,
    maxOutput: maxOutput && maxOutput > 0 ? Math.round(maxOutput) : null,
    ...pricing,
    ...parseUnitPricing(raw),
    tools: params ? params.includes('tools') : flag(raw.supports_tools ?? raw.capabilities?.function_calling ?? supports(raw.capabilities?.tools)),
    vision: Array.isArray(inputs) ? inputs.some((input) => input === 'image' || /^image\//i.test(String(input))) : flag(raw.supports_vision ?? raw.capabilities?.vision),
    json: params ? params.includes('response_format') || params.includes('structured_outputs') : null,
    reasoning: params ? params.includes('reasoning') || params.includes('include_reasoning') : flag(supports(raw.capabilities?.reasoning)),
    // System One decision models (POST /v1/systemone), listed under OpenRouter's decisions modality.
    decision: (raw.architecture?.output_modalities || []).includes('decisions'),
    paidOnly: isPaidOnly(raw),
    raw,
    ...overrides,
  };
}

module.exports = { normalizeModel, detectKind, parsePricing, parseUnitPricing, toNumber };
