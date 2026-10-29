'use strict';

const { OpenAICompatibleAdapter } = require('./openai_compatible');
const { normalizeModel } = require('../model_info');
const { ProviderError, KINDS } = require('../provider_error');

const API = 'https://api.cloudflare.com/client/v4';

function property(entry, id) {
  return (entry.properties || []).find((item) => item.property_id === id)?.value;
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
  async run(modelId, body, { signal, timeoutMs, binary = false }) {
    const response = await this.request(`${this.accountApi()}/ai/run/${modelId}`, {
      method: 'POST', headers: this.headers(), body, signal, timeoutMs, raw: true,
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
      const output = await this.run(modelId, { prompt, ...(width ? { width, height } : {}) }, { signal, timeoutMs });
      headers = output.headers;
      if (output.bytes) images.push({ b64: output.bytes.toString('base64'), url: null, mime: output.mime || 'image/png', revisedPrompt: null });
      else if (output.result?.image) images.push({ b64: output.result.image, url: null, mime: 'image/jpeg', revisedPrompt: null });
    }
    if (!images.length) throw new ProviderError(KINDS.EMPTY, 'Workers AI returned no image.');
    return { images, headers };
  }

  async speech({ input, voice }, { modelId, signal, timeoutMs }) {
    // MeloTTS takes a prompt and a language; Deepgram Aura takes text and a speaker.
    const body = /aura/i.test(modelId) ? { text: input, ...(voice && !OPENAI_VOICES.has(voice) ? { speaker: voice } : {}) } : { prompt: input, lang: 'en' };
    const output = await this.run(modelId, body, { signal, timeoutMs });
    if (output.bytes?.length) return { audio: output.bytes, mime: output.mime || 'audio/mpeg', headers: output.headers };
    const audio = output.result?.audio;
    if (!audio) throw new ProviderError(KINDS.EMPTY, 'Workers AI returned no audio.');
    return { audio: Buffer.from(audio, 'base64'), mime: 'audio/mpeg', headers: output.headers };
  }

  async transcribe({ file, language, translate = false }, { modelId, signal, timeoutMs }) {
    const body = /turbo|large-v3/.test(modelId)
      ? { audio: Buffer.from(file).toString('base64'), ...(language ? { language } : {}), ...(translate ? { task: 'translate' } : {}) }
      : { audio: [...Buffer.from(file)] };
    const output = await this.run(modelId, body, { signal, timeoutMs });
    const result = output.result || {};
    if (typeof result.text !== 'string') throw new ProviderError(KINDS.INVALID_OUTPUT, 'Workers AI returned no transcript.');
    const segments = Array.isArray(result.segments) ? result.segments.map((segment, id) => ({ id, start: segment.start, end: segment.end, text: segment.text })) : null;
    return { text: result.text, language: result.transcription_info?.language || language || null, duration: result.transcription_info?.duration ?? null, segments, headers: output.headers };
  }
}

const TASKS = [
  ['Text Generation', 'chat'],
  ['Text Embeddings', 'embedding'],
  ['Text-to-Image', 'image'],
  ['Automatic Speech Recognition', 'transcription'],
  ['Text-to-Speech', 'speech'],
];
const OPENAI_VOICES = new Set(['alloy', 'echo', 'fable', 'onyx', 'nova', 'shimmer', 'ash', 'coral', 'sage', 'ballad', 'verse']);

module.exports = { CloudflareAdapter };
