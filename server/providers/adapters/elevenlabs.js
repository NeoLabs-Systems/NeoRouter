'use strict';

// ElevenLabs: text-to-speech (/v1/text-to-speech/{voice_id}) and Scribe
// speech-to-text (/v1/speech-to-text). /v1/models lists the speech models;
// Scribe is not listed there, so the definition adds it.
const { BaseAdapter } = require('./base_adapter');
const { normalizeModel } = require('../model_info');
const { ProviderError, KINDS } = require('../provider_error');

// Eldrin, one of ElevenLabs' current default voices; only used when the
// account's voice lists cannot be read. (The older premade voices such as
// George exist only on accounts from before March 2026 and stop working at
// the end of 2026, so they are never assumed.)
const DEFAULT_VOICE = '6WwXjDDEMyNmFG95zycZ';
// OpenAI voice names → one of the account's premade voices with this voice type.
// Fixed voice ids are not used: older premade voices are Voice Library voices
// on newer accounts, which free accounts cannot use through the API.
const OPENAI_VOICE_GENDER = {
  alloy: 'female', echo: 'male', fable: 'male', onyx: 'male', nova: 'female', shimmer: 'female',
  ash: 'male', coral: 'female', sage: 'female', ballad: 'male', verse: 'male',
};
// Default and premade voices, and the ones the account made itself, work on
// every plan; Voice Library voices need a paid plan.
const usableVoice = (item) => item.category === 'premade' || item.category === 'cloned' || item.category === 'generated';
const libraryVoiceRefused = (error) => error instanceof ProviderError && error.kind === KINDS.PAYMENT && /library voice/i.test(error.message);
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

  // The account's voices, cached for a while. Not named `voices`: that is the method listing them below.
  async accountVoices() {
    if (!this.voiceCache || this.voiceCache.at < Date.now() - VOICE_CACHE_MS) {
      try {
        const { data } = await this.http('/voices', { timeoutMs: 15_000 });
        this.voiceCache = { at: Date.now(), list: data?.voices || [] };
      } catch {
        this.voiceCache = { at: Date.now(), list: [] };
      }
    }
    return this.voiceCache.list;
  }

  // ElevenLabs' current default voices (GET /v2/voices?voice_type=default), cached.
  async defaultVoices() {
    if (!this.defaultCache || this.defaultCache.at < Date.now() - VOICE_CACHE_MS) {
      try {
        const { data } = await this.http(`${this.baseUrl.replace(/\/v1$/, '')}/v2/voices?voice_type=default&page_size=100`, { timeoutMs: 15_000 });
        this.defaultCache = { at: Date.now(), list: data?.voices || [] };
      } catch {
        this.defaultCache = { at: Date.now(), list: [] };
      }
    }
    return this.defaultCache.list;
  }

  // Voices every plan can use: the defaults first, then the account's own premade and made voices.
  async usableVoices() {
    const [defaults, own] = await Promise.all([this.defaultVoices(), this.accountVoices()]);
    const seen = new Set();
    return [...defaults, ...own.filter(usableVoice)].filter((item) => item?.voice_id && !seen.has(item.voice_id) && seen.add(item.voice_id));
  }

  async defaultVoice() {
    return (await this.usableVoices())[0]?.voice_id || DEFAULT_VOICE;
  }

  // Voice ids and names of the account's voices are used as given; OpenAI
  // voice names get a premade voice of the same type; anything else the default.
  async voiceId(voice) {
    const value = String(voice || '').trim();
    if (!value) return this.defaultVoice();
    if (/^[A-Za-z0-9]{20}$/.test(value)) return value;
    const [own, usable] = await Promise.all([this.accountVoices(), this.usableVoices()]);
    const lower = value.toLowerCase();
    const named = [...usable, ...own].find((item) => String(item.name).toLowerCase() === lower || String(item.name).toLowerCase().startsWith(`${lower} `));
    if (named) return named.voice_id;
    const gender = OPENAI_VOICE_GENDER[lower];
    const matching = gender && usable.find((item) => String(item.labels?.gender || '').toLowerCase() === gender);
    return matching?.voice_id || usable[0]?.voice_id || DEFAULT_VOICE;
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
    const say = (voiceId) => this.request(this.url(`/text-to-speech/${encodeURIComponent(voiceId)}?output_format=${target.value}`), {
      method: 'POST', headers: this.headers({ Accept: target.mime }), body, signal, timeoutMs, raw: true,
    });
    const voiceId = await this.voiceId(voice);
    let response;
    try {
      response = await say(voiceId);
    } catch (error) {
      if (!libraryVoiceRefused(error)) throw error;
      // A Voice Library voice on a free plan: the account's own premade voice
      // instead. Still refused means this voice, not the account, is the problem.
      const fallback = await this.defaultVoice();
      if (fallback === voiceId) throw new ProviderError(KINDS.BAD_REQUEST, 'This ElevenLabs voice is from the Voice Library, which free accounts cannot use through the API.');
      try {
        response = await say(fallback);
      } catch (retryError) {
        if (libraryVoiceRefused(retryError)) throw new ProviderError(KINDS.BAD_REQUEST, 'ElevenLabs refused its own premade voice as a Voice Library voice; pick one of your voices.');
        throw retryError;
      }
    }
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
