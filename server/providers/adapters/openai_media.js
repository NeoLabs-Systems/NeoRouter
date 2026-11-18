'use strict';

// OpenAI-style endpoints for the non-chat kinds. A definition opts in per
// method with `media: { embed: '/embeddings', … }`; custom OpenAI-compatible
// endpoints support every method (the user knows what their server speaks).
// Every method returns a normalized result that the gateway turns back into
// the OpenAI response shape, so providers with other dialects (Gemini,
// Cloudflare) can return the same shapes.
const { ProviderError, KINDS } = require('../provider_error');
const { joinWav, pcmToWav } = require('./wav');
const { rivaSpeech, rivaVoices } = require('./riva_speech');

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

// OpenAI's voice names; other providers map them to voices of their own.
const OPENAI_VOICES = ['alloy', 'ash', 'ballad', 'coral', 'echo', 'fable', 'nova', 'onyx', 'sage', 'shimmer', 'verse'];

function voiceList(ids) {
  return ids.map((id) => ({ id, name: id.charAt(0).toUpperCase() + id.slice(1) }));
}

const methods = {
  supports(method) {
    if (method === 'chat') return true;
    return Boolean(mediaPath(this, method));
  },

  // input: string[] → one vector per input, in order.
  async embed({ input, dimensions, inputType }, { modelId, signal, timeoutMs }) {
    const body = { model: modelId, input, encoding_format: 'float' };
    if (dimensions) body[this.definition.quirks?.embeddingDimensionsField || 'dimensions'] = dimensions;
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

  // The voices this provider speaks for a model: listed by the provider, its
  // own fixed list, or OpenAI's names.
  async voices({ modelId } = {}) {
    if (this.definition.quirks?.speechApi === 'riva') return voiceList(rivaVoices(modelId));
    const { voices } = await speechVoices(this, modelId);
    return voiceList(voices || (this.definition.quirks?.speechVoicesPath ? [] : OPENAI_VOICES));
  },

  async speech({ input, voice, format = 'mp3', speed }, { modelId, signal, timeoutMs }) {
    const quirks = this.definition.quirks || {};
    const path = mediaPath(this, 'speech', modelId);
    if (quirks.speechApi === 'riva') return rivaSpeech(this, this.url(path), { input, voice }, { modelId, signal, timeoutMs });
    // A voice the model does not have becomes its default voice. A model whose
    // listing names no voices keeps the requested voice (or none), never an
    // OpenAI name the provider may not know.
    const { voices, defaultVoice } = await speechVoices(this, modelId);
    const known = voices?.find((name) => name.toLowerCase() === String(voice || '').toLowerCase());
    const chosenVoice = voices?.length ? known || defaultVoice || voices[0] : voice || defaultVoice || (quirks.speechVoicesPath ? undefined : 'alloy');
    // A format the provider can produce (the gateway converts when it can).
    const formats = quirks.speechFormats;
    const chosenFormat = formats && !formats.includes(format) ? formats[0] : format;
    // Hosts that cap the text of one request get it in sentence-sized parts.
    const pieces = quirks.maxSpeechChars && input.length > quirks.maxSpeechChars ? speechChunks(input, quirks.maxSpeechChars) : [input];
    const parts = [];
    for (const piece of pieces) {
      parts.push(await synthesize(this, path, { input: piece, voice: chosenVoice, speed }, chosenFormat, { modelId, signal, timeoutMs }));
    }
    const [first] = parts;
    if (isRawPcm(first)) {
      // Raw PCM (OpenRouter's default) has no header: wrapped into WAV unless PCM was asked for.
      const pcm = Buffer.concat(parts.map((part) => part.audio));
      if (format === 'pcm') return { audio: pcm, mime: first.mime, headers: first.headers };
      return { audio: pcmToWav(pcm, pcmRate(first.mime)), mime: 'audio/wav', headers: first.headers };
    }
    if (parts.length === 1) return { audio: first.audio, mime: first.mime, headers: first.headers };
    return { audio: joinWav(parts.map((part) => part.audio)), mime: 'audio/wav', headers: first.headers };
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
    if (seconds && this.definition.quirks?.videoDuration) body.duration = Math.round(Number(seconds));
    else if (seconds) body.seconds = String(seconds);
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
    const response = await this.request(this.url(`${mediaPath(this, 'createVideo')}/${encodeURIComponent(upstreamId)}/content`), { headers: this.headers(), signal, timeoutMs: 300_000, raw: true });
    return { response };
  },
};

// One speech request. A host that can produce several formats but refuses
// the chosen one for this model (OpenRouter: HeyGen only PCM, MiniMax only
// MP3) is asked once more for its other format.
async function synthesize(adapter, path, { input, voice, speed }, format, { modelId, signal, timeoutMs }) {
  const send = async (responseFormat) => {
    const body = { model: modelId, input, response_format: responseFormat };
    if (voice) body.voice = voice;
    if (speed) body.speed = speed;
    const response = await adapter.request(adapter.url(path), { method: 'POST', headers: adapter.headers(), body, signal, timeoutMs, raw: true });
    const audio = await binary(response);
    if (!audio.data.length) throw new ProviderError(KINDS.EMPTY, 'The provider returned no audio.');
    return { audio: audio.data, mime: audio.mime, headers: audio.headers, format: responseFormat };
  };
  const other = (adapter.definition.quirks?.speechFormats || []).find((item) => item !== format);
  try {
    return await send(format);
  } catch (error) {
    if (!other || !(error instanceof ProviderError) || error.kind !== KINDS.BAD_REQUEST) throw error;
    return send(other);
  }
}

// PCM as OpenAI and OpenRouter send it: 16-bit mono at 24 kHz unless the content type names a rate.
const PCM_RATE = 24000;
const isRawPcm = (part) => /pcm|l16/i.test(part.mime) || (part.format === 'pcm' && !/wav|mpeg|mp3|ogg|opus|aac|flac/i.test(part.mime));
const pcmRate = (mime) => Number(/rate=(\d+)/i.exec(mime || '')?.[1]) || PCM_RATE;

// Voices from a provider's model listing (OpenRouter's `supported_voices`),
// cached per listing URL. A listing that cannot be read is retried sooner.
const LISTED_VOICES_MS = 60 * 60 * 1000;
const LISTED_VOICES_RETRY_MS = 60 * 1000;
const listedVoiceCache = new Map();

async function listedVoices(adapter, path) {
  const url = adapter.url(path);
  const cached = listedVoiceCache.get(url);
  if (cached && cached.until > Date.now()) return cached.byModel;
  try {
    const { data } = await adapter.http(path, { timeoutMs: 30_000 });
    const byModel = new Map((Array.isArray(data?.data) ? data.data : [])
      .filter((entry) => entry?.id && Array.isArray(entry.supported_voices))
      .map((entry) => [entry.id, entry.supported_voices.map(String)]));
    listedVoiceCache.set(url, { until: Date.now() + LISTED_VOICES_MS, byModel });
    return byModel;
  } catch {
    const byModel = new Map();
    listedVoiceCache.set(url, { until: Date.now() + LISTED_VOICES_RETRY_MS, byModel });
    return byModel;
  }
}

// A model's voices and default voice (null: the first one): from the
// provider's listing, the definition's per-model voices or its fixed list.
async function speechVoices(adapter, modelId) {
  const quirks = adapter.definition.quirks || {};
  if (quirks.speechVoicesPath) {
    const byModel = await listedVoices(adapter, quirks.speechVoicesPath);
    const voices = byModel.get(modelId) || byModel.get(String(modelId || '').replace(/:free$/, '')) || null;
    return { voices: voices?.length ? voices : null, defaultVoice: null };
  }
  const forModel = quirks.modelVoices?.find((item) => new RegExp(item.match, 'i').test(modelId));
  return { voices: forModel?.voices || quirks.voices || null, defaultVoice: forModel?.defaultVoice || quirks.defaultVoice || null };
}

// Sentence-sized pieces of at most `max` characters. Sentences are packed
// together while they fit (fewer requests); a longer sentence is split
// between words, and a longer word wherever it must.
function speechChunks(text, max) {
  const units = [];
  for (const sentence of String(text).trim().split(/(?<=[.!?…;:])\s+/)) {
    if (sentence.length <= max) {
      units.push(sentence);
      continue;
    }
    let current = '';
    for (const word of sentence.split(/\s+/)) {
      for (const part of word.length > max ? word.match(new RegExp(`.{1,${max}}`, 'gu')) : [word]) {
        if (current && current.length + 1 + part.length > max) {
          units.push(current);
          current = '';
        }
        current = current ? `${current} ${part}` : part;
      }
    }
    if (current) units.push(current);
  }
  const chunks = [];
  for (const unit of units.filter(Boolean)) {
    const last = chunks.length - 1;
    if (last >= 0 && chunks[last].length + 1 + unit.length <= max) chunks[last] = `${chunks[last]} ${unit}`;
    else chunks.push(unit);
  }
  return chunks;
}

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

// NVIDIA's genai host: one image per call, base64 JPEG in artifacts[]. Its
// FLUX.2 models only make 1024x1024 images and refuse any other size, so the
// requested size is not passed on.
async function nvidiaGenai(adapter, path, { prompt, n = 1 }, { signal, timeoutMs }) {
  const [width, height] = [1024, 1024];
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

module.exports = { installOpenAIMedia, OPENAI_VOICES, voiceList, mediaPath, normalizeVideoStatus, aspectRatio, imageItem, speechChunks, DEFAULT_PATHS };
