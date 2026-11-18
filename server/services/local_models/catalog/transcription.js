'use strict';

// Speech to text (ONNX, run by transformers.js). Quantised weights: a quarter
// of the size with nearly the same accuracy.
// Whisper's tokenizer also needs merges.txt.
const SHARED = ['*.json', '*.txt'];

module.exports = [
  {
    id: 'whisper-base',
    name: 'Whisper Base',
    kind: 'transcription',
    engine: 'transformers',
    summary: {
      en: 'Fast transcription in many languages; fine for clear recordings.',
      de: 'Schnelle Transkription in vielen Sprachen; gut für klare Aufnahmen.',
    },
    source: { repo: 'onnx-community/whisper-base', include: [...SHARED, 'onnx/encoder_model_quantized.onnx', 'onnx/decoder_model_merged_quantized.onnx'] },
    downloadMb: 78,
    languages: 'many',
    license: 'MIT',
    transformers: { task: 'automatic-speech-recognition', dtype: 'q8' },
    features: { translate: true },
  },
  {
    id: 'whisper-small',
    name: 'Whisper Small',
    kind: 'transcription',
    engine: 'transformers',
    summary: {
      en: 'More accurate with accents and background noise; still quick.',
      de: 'Genauer bei Akzenten und Hintergrundgeräuschen; trotzdem flott.',
    },
    source: { repo: 'onnx-community/whisper-small', include: [...SHARED, 'onnx/encoder_model_quantized.onnx', 'onnx/decoder_model_merged_quantized.onnx'] },
    downloadMb: 241,
    languages: 'many',
    license: 'MIT',
    recommended: true,
    transformers: { task: 'automatic-speech-recognition', dtype: 'q8' },
    features: { translate: true },
  },
  {
    id: 'whisper-large-v3-turbo',
    name: 'Whisper Large v3 Turbo',
    kind: 'transcription',
    engine: 'transformers',
    summary: {
      en: 'Best accuracy for meetings, interviews and difficult audio.',
      de: 'Beste Genauigkeit für Meetings, Interviews und schwierige Aufnahmen.',
    },
    source: { repo: 'onnx-community/whisper-large-v3-turbo', include: [...SHARED, 'onnx/encoder_model_quantized.onnx', 'onnx/decoder_model_merged_quantized.onnx'] },
    downloadMb: 1040,
    languages: 'many',
    license: 'MIT',
    transformers: { task: 'automatic-speech-recognition', dtype: 'q8' },
    features: { translate: true },
  },
  {
    id: 'moonshine-base',
    name: 'Moonshine Base',
    kind: 'transcription',
    engine: 'transformers',
    summary: {
      en: 'Very fast English transcription for short voice messages and commands.',
      de: 'Sehr schnelle englische Transkription für kurze Sprachnachrichten und Befehle.',
    },
    source: { repo: 'onnx-community/moonshine-base-ONNX', include: ['*.json', 'onnx/encoder_model_quantized.onnx', 'onnx/decoder_model_merged_quantized.onnx'] },
    downloadMb: 64,
    languages: ['en'],
    license: 'MIT',
    transformers: { task: 'automatic-speech-recognition', dtype: 'q8' },
  },
];
