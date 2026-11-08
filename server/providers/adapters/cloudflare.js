'use strict';

const { OpenAICompatibleAdapter } = require('./openai_compatible');
const { normalizeModel } = require('../model_info');
const { ProviderError, KINDS } = require('../provider_error');

const API = 'https://api.cloudflare.com/client/v4';

function property(entry, id) {
  return (entry.properties || []).find((item) => item.property_id === id)?.value;
}

// Deepgram Aura's speakers per model. Aura refuses any other name (another
// provider's voice such as Gemini's "Kore"), so those get its default voice.
const AURA_VOICES = {
  'aura-2-en': ['amalthea', 'andromeda', 'apollo', 'arcas', 'aries', 'asteria', 'athena', 'atlas', 'aurora', 'callista', 'cora', 'cordelia', 'delia', 'draco', 'electra', 'harmonia', 'helena', 'hera', 'hermes', 'hyperion', 'iris', 'janus', 'juno', 'jupiter', 'luna', 'mars', 'minerva', 'neptune', 'odysseus', 'ophelia', 'orion', 'orpheus', 'pandora', 'phoebe', 'pluto', 'saturn', 'thalia', 'theia', 'vesta', 'zeus'],
  'aura-2-es': ['aquila', 'alvaro', 'carina', 'celeste', 'diana', 'estrella', 'javier', 'nestor', 'selena', 'sirio'],
  'aura-1': ['angus', 'arcas', 'asteria', 'athena', 'helios', 'hera', 'luna', 'orion', 'orpheus', 'perseus', 'stella', 'zeus'],
};

// FLUX.2 models only take multipart form data (their step count is fixed);
// the other image models take JSON.
function imageInput(modelId, prompt, width, height) {
  if (!/flux-2/i.test(modelId)) return { prompt, ...(width ? { width, height } : {}) };
  const form = new FormData();
  form.append('prompt', prompt);
  if (width) {
    form.append('width', String(width));
    form.append('height', String(height));
  }
  return form;
}

function auraSpeaker(modelId, voice) {
  const name = String(voice || '').toLowerCase();
  const voices = Object.entries(AURA_VOICES).find(([model]) => modelId.includes(model))?.[1];
  return voices?.includes(name) ? name : null;
}

class CloudflareAdapter extends OpenAICompatibleAdapter {
  async validate() {
    for (const pathname of ['/user/tokens/verify', `/accounts/${encodeURIComponent(this.options.accountId || '')}/tokens/verify`]) {
      try {
        const { data } = await this.http(`${API}${pathname}`, { timeoutMs: 20_000 });
        if (data?.result?.status === 'active') return { status: 'healthy', message: null };
        return { status: 'invalid', message: `Token status: ${data?.result?.status || 'unknown'}` };
      } catch (error) {
        if (error.kind !== KINDS.AUTH) return { status: 'unknown', message: error.message };
      }
    }
    return { status: 'invalid', message: 'Cloudflare rejected this API token.' };
  }

  async listModels() {
    const models = [];
    for (const [task, kind] of TASKS) {
      const { data } = await this.http(`${this.accountApi()}/ai/models/search?task=${encodeURIComponent(task)}&per_page=1000`, { timeoutMs: 30_000 });
      for (const entry of data?.result || []) {
        models.push(normalizeModel({ id: entry.name, name: entry.name, description: entry.description }, {
          ...(kind === 'chat' ? {} : { kind }),
          contextWindow: Number(property(entry, 'context_window')) || null,
          tools: String(property(entry, 'function_calling')) === 'true',
          vision: /vision|llava|-vl/i.test(entry.name),
        }));
      }
    }
    return models;
  }

  accountApi() {
    return `${API}/accounts/${encodeURIComponent(this.options.accountId || '')}`;
  }

  supports(method) {
    return ['chat', 'embed', 'generateImage', 'speech', 'transcribe'].includes(method);
  }

  // /ai/run answers JSON ({ result }) or raw bytes depending on the model.
  async run(modelId, body, { signal, timeoutMs, binary = false, headers = {} }) {
    const response = await this.request(`${this.accountApi()}/ai/run/${modelId}`, {
      method: 'POST', headers: this.headers(headers), body, signal, timeoutMs, raw: true,
    });
    const type = response.headers.get('content-type') || '';
    if (!/json/.test(type)) return { bytes: Buffer.from(await response.arrayBuffer()), mime: type, headers: response.headers };
    const data = await response.json();
    if (data?.success === false) throw new ProviderError(KINDS.BAD_REQUEST, data.errors?.[0]?.message || 'Workers AI rejected the request.');
    return { result: data?.result ?? data, headers: response.headers, binary };
  }

  async generateImage({ prompt, n = 1, size }, { modelId, signal, timeoutMs }) {
    const [width, height] = (/^(\d+)x(\d+)$/.exec(String(size || '')) || []).slice(1).map(Number);
    const images = [];
    let headers = null;
    for (let index = 0; index < Math.min(n, 4); index += 1) {
      const output = await this.run(modelId, imageInput(modelId, prompt, width, height), { signal, timeoutMs });
      headers = output.headers;
      if (output.bytes) images.push({ b64: output.bytes.toString('base64'), url: null, mime: output.mime || 'image/png', revisedPrompt: null });
      else if (output.result?.image) images.push({ b64: output.result.image, url: null, mime: 'image/jpeg', revisedPrompt: null });
    }
    if (!images.length) throw new ProviderError(KINDS.EMPTY, 'Workers AI returned no image.');
    return { images, headers };
  }

  async speech({ input, voice }, { modelId, signal, timeoutMs }) {
    // MeloTTS takes a prompt and a language; Deepgram Aura takes text and a speaker.
    const speaker = /aura/i.test(modelId) ? auraSpeaker(modelId, voice) : null;
    const body = /aura/i.test(modelId) ? { text: input, ...(speaker ? { speaker } : {}) } : { prompt: input, lang: 'en' };
    const output = await this.run(modelId, body, { signal, timeoutMs });
    if (output.bytes?.length) return { audio: output.bytes, mime: output.mime || 'audio/mpeg', headers: output.headers };
    const audio = output.result?.audio;
    if (!audio) throw new ProviderError(KINDS.EMPTY, 'Workers AI returned no audio.');
    return { audio: Buffer.from(audio, 'base64'), mime: 'audio/mpeg', headers: output.headers };
  }

  async transcribe({ file, mime, language, translate = false }, { modelId, signal, timeoutMs }) {
    if (/deepgram\/nova/.test(modelId)) return this.transcribeNova({ file, mime }, { modelId, signal, timeoutMs });
    const body = /turbo|large-v3/.test(modelId)
      ? { audio: Buffer.from(file).toString('base64'), ...(language ? { language } : {}), ...(translate ? { task: 'translate' } : {}) }
      : { audio: [...Buffer.from(file)] };
    const output = await this.run(modelId, body, { signal, timeoutMs });
    const result = output.result || {};
    if (typeof result.text !== 'string') throw new ProviderError(KINDS.INVALID_OUTPUT, 'Workers AI returned no transcript.');
    const segments = Array.isArray(result.segments) ? result.segments.map((segment, id) => ({ id, start: segment.start, end: segment.end, text: segment.text })) : null;
    return { text: result.text, language: result.transcription_info?.language || language || null, duration: result.transcription_info?.duration ?? null, segments, headers: output.headers };
  }

  // Deepgram Nova on Workers AI: its schema takes `audio: { body, contentType }`,
  // which over the REST API means the raw audio as the request body with its
  // content type (as Cloudflare's own Workers AI clients send it). Its options
  // (language, …) are not sent: the REST form for them is not documented.
  async transcribeNova({ file, mime }, { modelId, signal, timeoutMs }) {
    const output = await this.run(modelId, new Uint8Array(file), { signal, timeoutMs, headers: { 'Content-Type': mime || 'application/octet-stream' } });
    const channel = output.result?.results?.channels?.[0];
    const best = channel?.alternatives?.[0];
    if (typeof best?.transcript !== 'string') throw new ProviderError(KINDS.INVALID_OUTPUT, 'Workers AI returned no transcript.');
    return { text: best.transcript, language: channel.detected_language || null, duration: null, segments: null, headers: output.headers };
  }
}

const TASKS = [
  ['Text Generation', 'chat'],
  ['Text Embeddings', 'embedding'],
  ['Text-to-Image', 'image'],
  ['Automatic Speech Recognition', 'transcription'],
  ['Text-to-Speech', 'speech'],
];

module.exports = { CloudflareAdapter };
