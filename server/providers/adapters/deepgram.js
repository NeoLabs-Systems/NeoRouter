'use strict';

// Deepgram: Nova speech-to-text (/v1/listen, raw audio upload) and Aura
// text-to-speech (/v1/speak). Aura voices are separate model ids
// ("aura-2-thalia-en"); the catalog lists one Aura model and the request's
// voice picks the speaker.
const { BaseAdapter } = require('./base_adapter');
const { normalizeModel } = require('../model_info');
const { ProviderError, KINDS } = require('../provider_error');

const AURA_VOICES = new Set(['thalia', 'andromeda', 'helena', 'apollo', 'arcas', 'aries', 'asteria', 'athena', 'atlas', 'aurora', 'callista', 'cora', 'cordelia',
  'delia', 'draco', 'electra', 'harmonia', 'hera', 'hermes', 'hyperion', 'iris', 'janus', 'juno', 'jupiter', 'luna', 'mars', 'minerva', 'neptune', 'odysseus',
  'ophelia', 'orion', 'orpheus', 'pandora', 'phoebe', 'pluto', 'saturn', 'selene', 'theia', 'vesta', 'zeus']);
const OPENAI_VOICES = { alloy: 'thalia', echo: 'orion', fable: 'apollo', onyx: 'zeus', nova: 'luna', shimmer: 'asteria', ash: 'arcas', coral: 'cora', sage: 'athena', ballad: 'helena', verse: 'hermes' };
// Requested format → Deepgram encoding and container.
const ENCODINGS = {
  mp3: { encoding: 'mp3', mime: 'audio/mpeg' },
  opus: { encoding: 'opus', container: 'ogg', mime: 'audio/ogg' },
  wav: { encoding: 'linear16', container: 'wav', mime: 'audio/wav' },
  pcm: { encoding: 'linear16', container: 'none', sample_rate: '24000', mime: 'audio/pcm' },
  flac: { encoding: 'flac', mime: 'audio/flac' },
  aac: { encoding: 'aac', mime: 'audio/aac' },
};

function auraModel(family, voice) {
  const value = String(voice || '').toLowerCase();
  if (/^aura-/.test(value)) return value; // a full model id such as aura-2-julius-de
  const name = AURA_VOICES.has(value) ? value : OPENAI_VOICES[value] || 'thalia';
  return `${family}-${name}-en`;
}

class DeepgramAdapter extends BaseAdapter {
  authHeaders() {
    return this.secret ? { Authorization: `Token ${this.secret}` } : {};
  }

  supports(method) {
    return method === 'speech' || method === 'transcribe';
  }

  async listModels() {
    return (this.definition.models?.static || []).map((entry) => normalizeModel(entry, { kind: entry.kind }));
  }

  async voices() {
    return [...AURA_VOICES].map((id) => ({ id, name: id.charAt(0).toUpperCase() + id.slice(1) }));
  }

  async speech({ input, voice, format = 'mp3', speed }, { modelId, signal, timeoutMs }) {
    const target = ENCODINGS[format] || ENCODINGS.mp3;
    const params = new URLSearchParams({ model: auraModel(modelId, voice), encoding: target.encoding });
    if (target.container) params.set('container', target.container);
    if (target.sample_rate) params.set('sample_rate', target.sample_rate);
    if (speed) params.set('speed', String(speed));
    const response = await this.request(this.url(`/speak?${params}`), { method: 'POST', headers: this.headers(), body: { text: input }, signal, timeoutMs, raw: true });
    const audio = Buffer.from(await response.arrayBuffer());
    if (!audio.length) throw new ProviderError(KINDS.EMPTY, 'Deepgram returned no audio.');
    return { audio, mime: target.mime, headers: response.headers };
  }

  async transcribe({ file, mime, language, prompt }, { modelId, signal, timeoutMs }) {
    const params = new URLSearchParams({ model: modelId, smart_format: 'true', utterances: 'true', punctuate: 'true' });
    if (language) params.set('language', language);
    else params.set('detect_language', 'true');
    // Nova-3 takes up to 100 key terms as hints; the prompt's words serve as such.
    if (prompt && /nova-3/.test(modelId)) for (const term of prompt.split(/[\s,]+/).filter(Boolean).slice(0, 20)) params.append('keyterm', term);
    const { data, headers } = await this.http(`/listen?${params}`, {
      method: 'POST', headers: { 'Content-Type': mime || 'application/octet-stream' }, body: new Uint8Array(file), signal, timeoutMs,
    });
    const channel = data?.results?.channels?.[0];
    const text = channel?.alternatives?.[0]?.transcript;
    if (typeof text !== 'string') throw new ProviderError(KINDS.INVALID_OUTPUT, 'Deepgram returned no transcript.');
    const segments = (data.results.utterances || []).map((item, id) => ({ id, start: item.start, end: item.end, text: item.transcript }));
    return { text, language: channel.detected_language || language || null, duration: data.metadata?.duration ?? null, segments: segments.length ? segments : null, headers };
  }
}

// Deepgram cannot translate speech into English.
DeepgramAdapter.prototype.transcribeTranslate = false;

module.exports = { DeepgramAdapter, auraModel };
