'use strict';

// ElevenLabs: text-to-speech (/v1/text-to-speech/{voice_id}) and Scribe
// speech-to-text (/v1/speech-to-text). /v1/models lists the speech models;
// Scribe is not listed there, so the definition adds it.
const { BaseAdapter } = require('./base_adapter');
const { normalizeModel } = require('../model_info');
const { ProviderError, KINDS } = require('../provider_error');

const DEFAULT_VOICE = 'JBFqnCBsd6RMkjVDRZzb'; // George, a premade voice every account has
// OpenAI voice names → premade ElevenLabs voices.
const OPENAI_VOICES = {
  alloy: '21m00Tcm4TlvDq8ikWAM', echo: 'pNInz6obpgDQGcFmaJgB', fable: DEFAULT_VOICE, onyx: 'TxGEqnHWrfWFTfGW9XjX',
  nova: 'EXAVITQu4vr4xnSDxMaL', shimmer: 'XB0fDUnXU5powFXDhCwa',
};
const FORMATS = {
  mp3: { value: 'mp3_44100_128', mime: 'audio/mpeg' },
  opus: { value: 'opus_48000_64', mime: 'audio/ogg' },
  wav: { value: 'wav_24000', mime: 'audio/wav' },
  pcm: { value: 'pcm_24000', mime: 'audio/pcm' },
};
const VOICE_CACHE_MS = 10 * 60 * 1000;
// Subtitle cues: a new one after a sentence end, a pause or this many words.
const WORDS_PER_CUE = 14;

function cues(words) {
  const segments = [];
  let current = null;
  for (const word of words.filter((item) => item.type !== 'spacing' && item.type !== 'audio_event')) {
    if (!current || word.start - current.end > 1 || current.count >= WORDS_PER_CUE || /[.!?]$/.test(current.text)) {
      if (current) segments.push(current);
      current = { start: word.start, end: word.end, text: word.text, count: 1 };
    } else {
      current.text += ` ${word.text}`;
      current.end = word.end;
      current.count += 1;
    }
  }
  if (current) segments.push(current);
  return segments.map(({ start, end, text }, id) => ({ id, start, end, text }));
}

class ElevenLabsAdapter extends BaseAdapter {
  supports(method) {
    return method === 'speech' || method === 'transcribe';
  }

  async listModels() {
    const { data } = await this.http('/models', { timeoutMs: 30_000 });
    const speech = (Array.isArray(data) ? data : []).filter((model) => model.can_do_text_to_speech !== false)
      .map((model) => normalizeModel({ id: model.model_id, name: model.name }, { kind: 'speech' }));
    const extra = (this.definition.extraModels || []).map((entry) => normalizeModel(entry, { kind: entry.type }));
    return [...speech, ...extra];
  }

  // Voice ids, OpenAI voice names or the name of a voice in the account.
  async voiceId(voice) {
    const value = String(voice || '');
    if (!value) return DEFAULT_VOICE;
    if (OPENAI_VOICES[value.toLowerCase()]) return OPENAI_VOICES[value.toLowerCase()];
    if (/^[A-Za-z0-9]{20}$/.test(value)) return value;
    if (!this.voices || this.voices.at < Date.now() - VOICE_CACHE_MS) {
      try {
        const { data } = await this.http('/voices', { timeoutMs: 15_000 });
        this.voices = { at: Date.now(), list: data?.voices || [] };
      } catch {
        this.voices = { at: Date.now(), list: [] };
      }
    }
    return this.voices.list.find((item) => String(item.name).toLowerCase() === value.toLowerCase())?.voice_id || DEFAULT_VOICE;
  }

  // The account's voices, premade and its own, with what they sound like.
  async voices() {
    const { data } = await this.http('/voices', { timeoutMs: 15_000 });
    return (data?.voices || []).map((item) => ({
      id: item.voice_id,
      name: item.name,
      description: [item.labels?.gender, item.labels?.accent, item.labels?.description || item.labels?.descriptive].filter(Boolean).join(', ') || null,
    }));
  }

  async speech({ input, voice, format = 'mp3', speed }, { modelId, signal, timeoutMs }) {
    const target = FORMATS[format] || FORMATS.mp3;
    const body = { text: input, model_id: modelId };
    if (speed) body.voice_settings = { speed: Math.min(1.2, Math.max(0.7, speed)) };
    const voiceId = await this.voiceId(voice);
    const response = await this.request(this.url(`/text-to-speech/${encodeURIComponent(voiceId)}?output_format=${target.value}`), {
      method: 'POST', headers: this.headers({ Accept: target.mime }), body, signal, timeoutMs, raw: true,
    });
    const audio = Buffer.from(await response.arrayBuffer());
    if (!audio.length) throw new ProviderError(KINDS.EMPTY, 'ElevenLabs returned no audio.');
    return { audio, mime: target.mime, headers: response.headers };
  }

  async transcribe({ file, filename, mime, language }, { modelId, signal, timeoutMs }) {
    const form = new FormData();
    form.append('model_id', modelId);
    form.append('file', new Blob([file], { type: mime || 'application/octet-stream' }), filename || 'audio');
    form.append('timestamps_granularity', 'word');
    if (language) form.append('language_code', language);
    const { data, headers } = await this.http('/speech-to-text', { method: 'POST', body: form, signal, timeoutMs });
    if (typeof data?.text !== 'string') throw new ProviderError(KINDS.INVALID_OUTPUT, 'ElevenLabs returned no transcript.');
    const words = Array.isArray(data.words) ? data.words : [];
    const segments = cues(words);
    return { text: data.text, language: data.language_code || language || null, duration: words.length ? words[words.length - 1].end : null, segments: segments.length ? segments : null, headers };
  }
}

ElevenLabsAdapter.prototype.transcribeTranslate = false;

module.exports = { ElevenLabsAdapter, cues };
