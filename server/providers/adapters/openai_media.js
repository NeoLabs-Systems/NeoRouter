'use strict';

// OpenAI-style endpoints for the non-chat kinds. A definition opts in per
// method with `media: { embed: '/embeddings', … }`; custom OpenAI-compatible
// endpoints support every method (the user knows what their server speaks).
// Every method returns a normalized result that the gateway turns back into
// the OpenAI response shape, so providers with other dialects (Gemini,
// Cloudflare) can return the same shapes.
const { request } = require('../http');
const { ProviderError, KINDS } = require('../provider_error');

const DEFAULT_PATHS = Object.freeze({
  embed: '/embeddings',
  generateImage: '/images/generations',
  speech: '/audio/speech',
  transcribe: '/audio/transcriptions',
  moderate: '/moderations',
  createVideo: '/videos',
  decide: '/systemone',
});

function mediaPath(adapter, method, modelId) {
  const declared = adapter.definition.media?.[method];
  if (typeof declared === 'string') return declared.replace('{model}', modelId || '');
  if (adapter.definition.editableBaseUrl) return DEFAULT_PATHS[method];
  return null;
}

async function binary(response) {
  return { data: Buffer.from(await response.arrayBuffer()), mime: response.headers.get('content-type') || 'application/octet-stream', headers: response.headers };
}

const methods = {
  supports(method) {
    if (method === 'chat') return true;
    return Boolean(mediaPath(this, method));
  },

  // input: string[] → one vector per input, in order.
  async embed({ input, dimensions, inputType }, { modelId, signal, timeoutMs }) {
    const body = { model: modelId, input, encoding_format: 'float' };
    if (dimensions) body.dimensions = dimensions;
    // NVIDIA's retrieval embedders need to know whether they embed a query or a passage.
    if (this.definition.quirks?.embeddingInputType) body.input_type = inputType || 'query';
    const { data, headers } = await this.http(mediaPath(this, 'embed'), { method: 'POST', body, signal, timeoutMs });
    const rows = Array.isArray(data?.data) ? [...data.data].sort((a, b) => (a.index ?? 0) - (b.index ?? 0)) : [];
    if (rows.length !== input.length || !rows.every((row) => Array.isArray(row.embedding))) {
      throw new ProviderError(KINDS.INVALID_OUTPUT, 'The provider returned an unexpected embeddings response.');
    }
    return { vectors: rows.map((row) => row.embedding), inputTokens: data.usage?.prompt_tokens ?? null, headers };
  },

  async generateImage({ prompt, n = 1, size, quality }, { modelId, signal, timeoutMs }) {
    const path = mediaPath(this, 'generateImage', modelId);
    if (this.definition.quirks?.imageApi === 'nvidia_genai') return nvidiaGenai(this, path, { prompt, n, size }, { signal, timeoutMs });
    const body = { model: modelId, prompt, n };
    if (this.definition.quirks?.noImageResponseFormat) {
      // OpenRouter takes an aspect ratio instead of pixel sizes.
      if (size) body.aspect_ratio = aspectRatio(size);
    } else {
      if (size) body.size = size;
      body.response_format = 'b64_json';
    }
    if (quality) body.quality = quality;
    const { data, headers } = await this.http(path, { method: 'POST', body, signal, timeoutMs });
    const rows = data?.data || data?.images || [];
    const images = rows.map((item) => imageItem(item.b64_json || item.url || item.image_url?.url || item.image_url, item.revised_prompt))
      .filter((item) => item.b64 || item.url);
    if (!images.length) throw new ProviderError(KINDS.EMPTY, 'The provider returned no image.');
    return { images, headers };
  },

  async speech({ input, voice, format = 'mp3', speed }, { modelId, signal, timeoutMs }) {
    const quirks = this.definition.quirks || {};
    // Providers with their own voices fall back to their default voice, and
    // to a format they can produce (the gateway converts when it can).
    const chosenVoice = quirks.voices && !quirks.voices.includes(voice) ? quirks.defaultVoice || quirks.voices[0] : voice || quirks.defaultVoice || 'alloy';
    const chosenFormat = quirks.speechFormats && !quirks.speechFormats.includes(format) ? quirks.speechFormats[0] : format;
    const body = { model: modelId, input, voice: chosenVoice, response_format: chosenFormat };
    if (speed) body.speed = speed;
    const response = await request(this.url(mediaPath(this, 'speech')), { method: 'POST', headers: this.headers(), body, signal, timeoutMs, raw: true });
    const audio = await binary(response);
    if (!audio.data.length) throw new ProviderError(KINDS.EMPTY, 'The provider returned no audio.');
    return { audio: audio.data, mime: audio.mime, headers: audio.headers };
  },

  async transcribe({ file, filename, mime, language, prompt, translate = false }, { modelId, signal, timeoutMs }) {
    if (this.definition.quirks?.transcriptionJson) {
      // OpenRouter takes the audio as base64 JSON.
      const body = { model: modelId, input_audio: { data: Buffer.from(file).toString('base64'), format: audioFormat(filename, mime) } };
      if (language) body.language = language;
      if (prompt) body.prompt = prompt;
      const { data, headers } = await this.http(mediaPath(this, 'transcribe'), { method: 'POST', body, signal, timeoutMs });
      if (typeof data?.text !== 'string') throw new ProviderError(KINDS.INVALID_OUTPUT, 'The provider returned no transcript.');
      return { text: data.text, language: data.language || language || null, duration: data.duration ?? null, segments: data.segments || null, headers };
    }
    const form = new FormData();
    form.append('file', new Blob([file], { type: mime || 'application/octet-stream' }), filename || 'audio');
    form.append('model', modelId);
    form.append('response_format', 'verbose_json');
    if (language && !translate) form.append('language', language);
    if (prompt) form.append('prompt', prompt);
    const path = translate ? mediaPath(this, 'transcribe').replace('transcriptions', 'translations') : mediaPath(this, 'transcribe');
    const { data, headers } = await this.http(path, { method: 'POST', body: form, signal, timeoutMs });
    const result = typeof data === 'string' ? { text: data } : data || {};
    if (typeof result.text !== 'string') throw new ProviderError(KINDS.INVALID_OUTPUT, 'The provider returned no transcript.');
    return { text: result.text, language: result.language || language || null, duration: result.duration ?? null, segments: result.segments || null, headers };
  },

  // System One decision models (Jev and others): typed answers to questions
  // about a state, in TypeSafe's request shape, which OpenRouter also speaks.
  async decide({ state, questions }, { modelId, signal, timeoutMs }) {
    const { data, headers } = await this.http(mediaPath(this, 'decide'), { method: 'POST', body: { model: modelId, state, questions }, signal, timeoutMs });
    if (!data?.answers || typeof data.answers !== 'object') throw new ProviderError(KINDS.INVALID_OUTPUT, 'The decision model returned no answers.');
    return { answers: data.answers, usage: data.usage || {}, inputTokens: data.usage?.prompt_tokens ?? data.usage?.input_tokens, headers };
  },

  async moderate({ input }, { modelId, signal, timeoutMs }) {
    const { data, headers } = await this.http(mediaPath(this, 'moderate'), { method: 'POST', body: { model: modelId, input }, signal, timeoutMs });
    if (!Array.isArray(data?.results)) throw new ProviderError(KINDS.INVALID_OUTPUT, 'The provider returned an unexpected moderation response.');
    return { results: data.results, headers };
  },

  // Videos are jobs: create returns an id that only this provider knows.
  async createVideo({ prompt, seconds, size }, { modelId, signal, timeoutMs }) {
    const body = { model: modelId, prompt };
    if (seconds) body.seconds = String(seconds);
    if (size) body.size = size;
    const { data, headers } = await this.http(mediaPath(this, 'createVideo'), { method: 'POST', body, signal, timeoutMs });
    if (!data?.id) throw new ProviderError(KINDS.INVALID_OUTPUT, 'The provider returned no video job id.');
    return { upstreamId: data.id, status: normalizeVideoStatus(data.status), progress: data.progress ?? null, headers };
  },

  async getVideo(upstreamId, { signal } = {}) {
    const { data } = await this.http(`${mediaPath(this, 'createVideo')}/${encodeURIComponent(upstreamId)}`, { signal, timeoutMs: 30_000 });
    return { status: normalizeVideoStatus(data?.status), progress: data?.progress ?? null, error: data?.error?.message || null };
  },

  async videoContent(upstreamId, { signal } = {}) {
    const response = await request(this.url(`${mediaPath(this, 'createVideo')}/${encodeURIComponent(upstreamId)}/content`), { headers: this.headers(), signal, timeoutMs: 300_000, raw: true });
    return { response };
  },
};

function imageItem(value, revisedPrompt = null) {
  const text = String(value || '');
  const match = /^data:([^;]+);base64,(.*)$/s.exec(text);
  if (match) return { b64: match[2], url: null, mime: match[1], revisedPrompt };
  if (/^https?:\/\//.test(text)) return { b64: null, url: text, mime: null, revisedPrompt };
  return { b64: text || null, url: null, mime: 'image/png', revisedPrompt };
}

function aspectRatio(size) {
  const match = /^(\d+)x(\d+)$/.exec(String(size));
  if (!match) return String(size);
  const [w, h] = [Number(match[1]), Number(match[2])];
  const gcd = (a, b) => (b ? gcd(b, a % b) : a);
  const d = gcd(w, h);
  return `${w / d}:${h / d}`;
}

function audioFormat(filename, mime) {
  const ext = /\.(\w+)$/.exec(filename || '')?.[1]?.toLowerCase();
  if (ext) return ext === 'mpeg' ? 'mp3' : ext;
  return String(mime || '').split('/')[1]?.replace('mpeg', 'mp3').replace('x-wav', 'wav') || 'wav';
}

// NVIDIA's genai host: one image per call, base64 JPEG in artifacts[].
async function nvidiaGenai(adapter, path, { prompt, n = 1, size }, { signal, timeoutMs }) {
  const [width, height] = (/^(\d+)x(\d+)$/.exec(String(size || '')) || [null, 1024, 1024]).slice(1).map(Number);
  const images = [];
  let headers = null;
  for (let index = 0; index < Math.min(n, 4); index += 1) {
    const result = await adapter.http(path, { method: 'POST', headers: { Accept: 'application/json' }, body: { prompt, width, height, seed: Math.floor(Math.random() * 1e9) }, signal, timeoutMs });
    headers = result.headers;
    const artifact = result.data?.artifacts?.[0];
    if (artifact?.finishReason && artifact.finishReason !== 'SUCCESS') throw new ProviderError(KINDS.BAD_REQUEST, `The image was blocked (${artifact.finishReason}).`);
    if (artifact?.base64) images.push({ b64: artifact.base64, url: null, mime: 'image/jpeg', revisedPrompt: null });
  }
  if (!images.length) throw new ProviderError(KINDS.EMPTY, 'The provider returned no image.');
  return { images, headers };
}

function normalizeVideoStatus(status) {
  const value = String(status || '').toLowerCase();
  if (['completed', 'succeeded', 'success', 'done'].includes(value)) return 'completed';
  if (['failed', 'error', 'cancelled', 'canceled'].includes(value)) return 'failed';
  if (['in_progress', 'running', 'processing'].includes(value)) return 'in_progress';
  return 'queued';
}

function installOpenAIMedia(AdapterClass) {
  for (const [name, fn] of Object.entries(methods)) {
    if (!Object.prototype.hasOwnProperty.call(AdapterClass.prototype, name)) AdapterClass.prototype[name] = fn;
  }
}

module.exports = { installOpenAIMedia, mediaPath, normalizeVideoStatus, aspectRatio, imageItem, DEFAULT_PATHS };
