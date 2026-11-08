'use strict';

// NVIDIA Riva text-to-speech as OVHcloud AI Endpoints hosts it: one host per
// model (`media.speech` names it with {model}), JSON in, 16-bit WAV out.
//
// Riva lists its voices only over gRPC; this HTTP API has no voice listing
// (GET /api/v1/tts/list_voices answers 404), so the voices are derived from
// the language in the model id (nvr-tts-de-de → German, DE):
//   - English is Riva's multi-speaker model, "English-US.Female-1" (the
//     default in OVHcloud's docs) or "English-US.Male-1".
//   - The other languages have one model per voice, "German-DE-Male-1",
//     "Spanish-ES-Female-1", and not every language has both.
// A derived voice Riva does not know falls back to the other one, and the
// voice that worked is remembered per model.
const { ProviderError, KINDS } = require('../provider_error');

const SAMPLE_RATE_HZ = 16000;
// Riva's AudioEncoding.LINEAR_PCM.
const LINEAR_PCM = 1;
const LANGUAGE_NAMES = new Intl.DisplayNames(['en'], { type: 'language' });
// Model URL → the derived voice that worked there.
const workingVoice = new Map();

/** { language, region } from a Riva model id such as nvr-tts-de-de, or null. */
function modelLanguage(modelId) {
  const match = /(?:^|[-_/])([a-z]{2,3})-([a-z]{2})$/i.exec(String(modelId || ''));
  return match ? { language: match[1].toLowerCase(), region: match[2].toUpperCase() } : null;
}

/** The voices derived for a model: female first, as OVHcloud's docs default to it. */
function rivaVoices(modelId) {
  const lang = modelLanguage(modelId);
  if (!lang) return [];
  const name = LANGUAGE_NAMES.of(lang.language);
  const separator = lang.language === 'en' ? '.' : '-';
  return ['Female-1', 'Male-1'].map((speaker) => `${name}-${lang.region}${separator}${speaker}`);
}

// A full Riva voice name such as "English-US.Male-1" is passed on as it is;
// anything else (an OpenAI or Gemini voice name) gets the derived voices.
const isRivaVoiceName = (voice) => /^[A-Z][a-z]+-[A-Z]{2}[.-]/.test(String(voice || ''));
const unknownVoice = (error) => error instanceof ProviderError && error.kind === KINDS.BAD_REQUEST && /voice .* not found/i.test(error.message);

async function rivaSpeech(adapter, url, { input, voice }, { modelId, signal, timeoutMs }) {
  const lang = modelLanguage(modelId);
  if (!lang) throw new ProviderError(KINDS.BAD_REQUEST, `The language of the Riva model ${modelId} is not known.`);
  const say = async (voiceName) => {
    const body = { encoding: LINEAR_PCM, language_code: `${lang.language}-${lang.region}`, sample_rate_hz: SAMPLE_RATE_HZ, text: input, voice_name: voiceName };
    const response = await adapter.request(url, { method: 'POST', headers: adapter.headers(), body, signal, timeoutMs, raw: true });
    const audio = Buffer.from(await response.arrayBuffer());
    if (!audio.length) throw new ProviderError(KINDS.EMPTY, 'The provider returned no audio.');
    // The host labels the WAV as application/octet-stream.
    return { audio, mime: 'audio/wav', headers: response.headers };
  };
  if (isRivaVoiceName(voice)) return say(voice);
  const remembered = workingVoice.get(url);
  const candidates = remembered ? [remembered] : rivaVoices(modelId);
  for (const [index, candidate] of candidates.entries()) {
    try {
      const result = await say(candidate);
      workingVoice.set(url, candidate);
      return result;
    } catch (error) {
      if (!unknownVoice(error) || index === candidates.length - 1) throw error;
    }
  }
  throw new ProviderError(KINDS.BAD_REQUEST, `No voice is known for the Riva model ${modelId}.`);
}

module.exports = { rivaSpeech, rivaVoices, modelLanguage };
