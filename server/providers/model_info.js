'use strict';

// Normalizes the many shapes of `/models` entries into one record. Prices are
// USD per 1M tokens. `priceKnown` is only true when the provider itself
// reported a price; `freeHint` is only true when every reported price field is
// exactly zero or the provider labels the model free. Unknown never means free.

const NON_CHAT_PATTERNS = [
  ['embedding', /embed|bge-|e5-|gte-|nomic-embed|text-similarity/i],
  ['rerank', /rerank/i],
  ['audio', /whisper|tts|speech|transcri|audio|voice|orpheus|playai|lyria|music/i],
  ['image', /dall-e|stable-diffusion|sdxl|flux|imagen|image-gen|gpt-image|kolors|seedream|nano-banana|-image(-|$)/i],
  ['video', /\bvideo\b|veo-|sora|wan2|kling|hailuo/i],
  ['moderation', /moderation|llama-guard|shieldgemma|prompt-guard|safeguard|content-safety|nemoguard|safety-guard|topic-control/i],
  // Agents, tool-only and legacy multimodal endpoints that do not accept plain chat.
  ['other', /deep-research|computer-use|robotics|antigravity|deplot|kosmos|fuyu|calibration|reward|embedqa|parse|translat|\bocr\b|-ocr|retriever/i],
];

function detectKind(id, raw = {}) {
  const declared = String(raw.type || raw.object_type || raw.task || '').toLowerCase();
  if (/embed/.test(declared)) return 'embedding';
  if (/image/.test(declared) && !/text/.test(declared)) return 'image';
  const outputs = raw.architecture?.output_modalities;
  if (Array.isArray(outputs) && outputs.length && !outputs.includes('text')) {
    return outputs.includes('image') ? 'image' : outputs.includes('audio') ? 'audio' : 'other';
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

function parsePricing(raw) {
  if (raw.price === 'free' || raw.pricing === 'free' || raw.is_free === true || raw.free === true) {
    return { priceIn: 0, priceOut: 0, priceKnown: true, freeHint: true };
  }
  const pricing = raw.pricing && typeof raw.pricing === 'object' ? raw.pricing : null;
  if (pricing) {
    const values = Object.values(pricing).map(toNumber).filter((value) => value !== null);
    const unit = pricing.unit === 'token' ? 'token' : undefined;
    const prompt = toNumber(pricing.prompt ?? pricing.input ?? pricing.input_tokens);
    const completion = toNumber(pricing.completion ?? pricing.output ?? pricing.output_tokens);
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

function supportedParameters(raw) {
  return Array.isArray(raw.supported_parameters) ? raw.supported_parameters : null;
}

function normalizeModel(raw, overrides = {}) {
  const id = String(raw.id || raw.name || raw.model || '').trim();
  const params = supportedParameters(raw);
  const inputs = raw.architecture?.input_modalities || raw.input_modalities || raw.modalities?.input;
  const contextWindow = toNumber(raw.context_length ?? raw.context_window ?? raw.max_context_length
    ?? raw.top_provider?.context_length ?? raw.max_model_len ?? raw.inputTokenLimit);
  const maxOutput = toNumber(raw.max_output_tokens ?? raw.top_provider?.max_completion_tokens ?? raw.max_completion_tokens
    ?? raw.outputTokenLimit);
  const pricing = parsePricing(raw);
  if (/:free$/.test(id)) {
    pricing.freeHint = true;
    pricing.priceKnown = true;
    pricing.priceIn = 0;
    pricing.priceOut = 0;
  }
  const flag = (value) => (value === undefined || value === null ? null : Boolean(value));
  return {
    id,
    name: raw.display_name || raw.displayName || raw.name || id,
    kind: detectKind(id, raw),
    contextWindow: contextWindow && contextWindow > 0 ? Math.round(contextWindow) : null,
    maxOutput: maxOutput && maxOutput > 0 ? Math.round(maxOutput) : null,
    ...pricing,
    tools: params ? params.includes('tools') : flag(raw.supports_tools ?? raw.capabilities?.function_calling ?? raw.capabilities?.tools),
    vision: Array.isArray(inputs) ? inputs.includes('image') : flag(raw.supports_vision ?? raw.capabilities?.vision),
    json: params ? params.includes('response_format') || params.includes('structured_outputs') : null,
    reasoning: params ? params.includes('reasoning') || params.includes('include_reasoning') : flag(raw.capabilities?.reasoning),
    raw,
    ...overrides,
  };
}

module.exports = { normalizeModel, detectKind, parsePricing, toNumber };
