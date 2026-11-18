'use strict';

// The models running on this server (services/local_models). Chat, safety
// and embedding models are llama.cpp servers on 127.0.0.1 that speak OpenAI's
// API, so those calls go to the OpenAI-compatible adapter pointed at the
// model's own port; speech and transcription models run in a worker process.
// A model that is not running right now is a temporary server error, so the
// router tries something else.
const { BaseAdapter } = require('./base_adapter');
const { OpenAICompatibleAdapter } = require('./openai_compatible');
const { normalizeModel } = require('../model_info');
const { ProviderError, KINDS } = require('../provider_error');

// Required lazily: the service needs the provider registry, which loads this file.
const service = () => require('../../services/local_models/local_model_service');
const access = () => require('../../services/local_models/access');
const audio = () => require('../../services/local_models/audio');
const catalog = () => require('../../services/local_models/catalog');

const NOT_RUNNING_RETRY_MS = 30_000;
// llama.cpp's own paths for what it serves besides chat.
const UPSTREAM_MEDIA = Object.freeze({ embed: '/embeddings' });

class LocalRuntimeAdapter extends BaseAdapter {
  // Only some speech recognisers translate into English (Whisper does, Moonshine does not).
  canTranslate(modelId) {
    return Boolean(catalog().get(modelId)?.features.translate);
  }

  allowed() {
    return access().allowed(this.account.userId);
  }

  instance(modelId) {
    if (!this.allowed()) throw new ProviderError(KINDS.AUTH, 'An admin has not shared this server’s local models with you.', { needsAction: true });
    const instance = service().runningInstance(modelId);
    if (!instance) throw new ProviderError(KINDS.SERVER, 'This local model is not running right now.', { retryAfterMs: NOT_RUNNING_RETRY_MS });
    return instance;
  }

  // The running llama.cpp server of a model, as an OpenAI-compatible connection.
  upstream(modelId) {
    const { endpoint } = this.instance(modelId);
    if (!endpoint) throw new ProviderError(KINDS.BAD_REQUEST, 'This local model does not answer this kind of request.');
    return new OpenAICompatibleAdapter({ ...this.definition, media: UPSTREAM_MEDIA }, { baseUrl: endpoint.baseUrl, secret: endpoint.token, options: {}, publicOnly: false });
  }

  worker(modelId) {
    const { invoke } = this.instance(modelId);
    if (!invoke) throw new ProviderError(KINDS.BAD_REQUEST, 'This local model does not answer this kind of request.');
    return invoke;
  }

  // The check before a connection is created has no owner yet; who may add one is decided by the route.
  async validate() {
    if (this.account.userId && !this.allowed()) return { status: 'invalid', message: 'An admin has not shared this server’s local models with you.' };
    const count = service().servedModels().length;
    return { status: 'healthy', message: count ? `${count} local model${count === 1 ? '' : 's'} running.` : 'No local model is running right now.' };
  }

  async listModels() {
    if (!this.allowed()) return [];
    return service().servedModels().map(({ entry, contextWindow }) => normalizeModel({ id: entry.id, name: entry.name }, {
      kind: entry.kind,
      contextWindow: contextWindow || null,
      tools: entry.features.tools,
      vision: entry.features.vision,
      reasoning: entry.features.reasoning,
      priceKnown: true,
      priceIn: 0,
      priceOut: 0,
      freeHint: true,
    }));
  }

  chat(canonical, context) {
    return this.upstream(context.modelId).chat(canonical, context);
  }

  stream(canonical, context) {
    return this.upstream(context.modelId).stream(canonical, context);
  }

  embed(input, context) {
    return this.upstream(context.modelId).embed(input, context);
  }

  async voices({ modelId } = {}) {
    const entry = catalog().get(modelId);
    const voices = (entry?.voices || []).map((voice) => ({ id: voice.id, name: voice.name }));
    return { voices: voices.length ? voices : null, defaultVoice: voices[0]?.id || null };
  }

  // WAV out; the gateway converts it to the format the client asked for.
  async speech({ input, voice, speed }, { modelId, signal, timeoutMs }) {
    const invoke = this.worker(modelId);
    const known = (await this.voices({ modelId })).voices || [];
    const chosen = known.find((item) => item.id === String(voice || '').toLowerCase())?.id || known[0]?.id || null;
    try {
      const { samples, rate } = await invoke('speech', { text: input, voice: chosen, speed }, { signal, timeoutMs });
      return { audio: audio().toWav(samples, rate), mime: 'audio/wav', headers: new Headers() };
    } catch (error) {
      if (signal?.aborted) throw new ProviderError(KINDS.ABORTED, 'The request was cancelled.');
      throw new ProviderError(KINDS.SERVER, error.message);
    }
  }

  async transcribe({ file, language, translate = false }, { modelId, signal, timeoutMs }) {
    const invoke = this.worker(modelId);
    const samples = await audio().toSamples(file);
    if (!samples?.length) throw new ProviderError(KINDS.BAD_REQUEST, 'This audio could not be read. Send WAV, or install ffmpeg on the server for other formats.');
    try {
      const result = await invoke('transcribe', { samples, language: language || null, translate }, { signal, timeoutMs });
      return { text: result.text, language: result.language, duration: result.duration, segments: result.segments, headers: new Headers() };
    } catch (error) {
      if (signal?.aborted) throw new ProviderError(KINDS.ABORTED, 'The request was cancelled.');
      throw new ProviderError(KINDS.SERVER, error.message);
    }
  }
}

module.exports = { LocalRuntimeAdapter };
