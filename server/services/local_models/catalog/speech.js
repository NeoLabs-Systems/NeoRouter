'use strict';

// Text to speech (ONNX, run by transformers.js).
const SUPERTONIC_VOICES = ['F1', 'F2', 'F3', 'F4', 'F5', 'M1', 'M2', 'M3', 'M4', 'M5']
  .map((id) => ({ id: id.toLowerCase(), name: `${id.startsWith('F') ? 'Female' : 'Male'} ${id.slice(1)}`, file: `voices/${id}.bin` }));

// Meta's MMS voices: one per language, small, licensed for non-commercial use only.
const mms = (language, code, name, summary) => ({
  id: `mms-tts-${code}`,
  name: `MMS ${name}`,
  kind: 'speech',
  engine: 'transformers',
  summary,
  source: { repo: `Xenova/mms-tts-${code}`, include: ['*.json', 'onnx/model_quantized.onnx'] },
  downloadMb: 37,
  languages: [language],
  license: 'CC-BY-NC-4.0',
  transformers: { task: 'text-to-speech', dtype: 'q8' },
  voices: [{ id: 'default', name }],
});

module.exports = [
  {
    id: 'supertonic-tts',
    name: 'Supertonic',
    kind: 'speech',
    engine: 'transformers',
    summary: {
      en: 'Natural English voices, ten to choose from, faster than real time on a CPU.',
      de: 'Natürliche englische Stimmen, zehn zur Auswahl, schneller als Echtzeit auf einer CPU.',
    },
    source: { repo: 'onnx-community/Supertonic-TTS-ONNX', include: ['*.json', 'onnx/*', 'voices/*.bin'] },
    downloadMb: 251,
    languages: ['en'],
    license: 'OpenRAIL',
    recommended: true,
    transformers: { task: 'text-to-speech' },
    voices: SUPERTONIC_VOICES,
  },
  mms('en', 'eng', 'English', {
    en: 'Simple, very light English voice. Non-commercial use only.',
    de: 'Einfache, sehr leichte englische Stimme. Nur für nicht-kommerzielle Nutzung.',
  }),
  mms('de', 'deu', 'Deutsch', {
    en: 'Simple, very light German voice. Non-commercial use only.',
    de: 'Einfache, sehr leichte deutsche Stimme. Nur für nicht-kommerzielle Nutzung.',
  }),
];
