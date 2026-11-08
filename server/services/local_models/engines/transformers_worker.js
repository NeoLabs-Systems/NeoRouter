'use strict';

// Runs one transformers.js model (speech or transcription) in its own
// process, so a crash or a memory spike never takes NeoRouter down. Talks to
// its parent over IPC: { type: 'ready' } once loaded, then one reply per
// request { id, ok, result | error }. Only files already on disk are used.
const fs = require('node:fs');

const config = JSON.parse(process.env.NEOROUTER_LOCAL_MODEL || '{}');
// Text-to-speech tokenizers truncate long input, so text is spoken in parts.
const MAX_SPEECH_CHARS = 300;

// Sentences first, then words, so no part is longer than `max`.
function speechParts(text, max = MAX_SPEECH_CHARS) {
  const sentences = String(text).replace(/\s+/g, ' ').trim().match(/[^.!?。！？]+[.!?。！？]*\s*/g) || [];
  const parts = [];
  let current = '';
  for (const sentence of sentences) {
    for (const piece of sentence.length > max ? sentence.match(new RegExp(`.{1,${max}}(\\s|$)`, 'g')) : [sentence]) {
      if ((current + piece).length > max && current) {
        parts.push(current.trim());
        current = '';
      }
      current += piece;
    }
  }
  if (current.trim()) parts.push(current.trim());
  return parts;
}

function voiceEmbedding(file) {
  const bytes = fs.readFileSync(file);
  return new Float32Array(bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.length));
}

async function main() {
  const { pipeline, env } = await import('@huggingface/transformers');
  env.localModelPath = config.modelsDir;
  env.allowLocalModels = true;
  env.allowRemoteModels = false;
  const pipe = await pipeline(config.task, config.id, { ...(config.dtype ? { dtype: config.dtype } : {}) });
  const isWhisper = pipe.model?.config?.model_type === 'whisper';
  const voices = new Map();

  const handlers = {
    ping: async () => ({ ok: true }),
    async speech({ text, voice, speed }) {
      const options = {};
      const file = config.voices?.[voice] || Object.values(config.voices || {})[0];
      if (file) {
        if (!voices.has(file)) voices.set(file, voiceEmbedding(file));
        options.speaker_embeddings = voices.get(file);
        if (speed) options.speed = speed;
      }
      const chunks = [];
      let rate = null;
      for (const part of speechParts(text)) {
        const output = await pipe(part, options);
        rate = output.sampling_rate;
        chunks.push(output.audio);
      }
      const total = chunks.reduce((sum, chunk) => sum + chunk.length, 0);
      const samples = new Float32Array(total);
      let offset = 0;
      for (const chunk of chunks) {
        samples.set(chunk, offset);
        offset += chunk.length;
      }
      return { samples, rate };
    },
    // samples: 16 kHz mono Float32Array.
    async transcribe({ samples, language, translate }) {
      const options = { chunk_length_s: 30, stride_length_s: 5, return_timestamps: true };
      if (isWhisper) {
        if (language) options.language = language;
        options.task = translate ? 'translate' : 'transcribe';
      }
      const output = await pipe(samples, options);
      const segments = (output.chunks || []).map((chunk) => ({ start: chunk.timestamp?.[0] ?? 0, end: chunk.timestamp?.[1] ?? null, text: chunk.text }));
      return { text: String(output.text || '').trim(), segments, duration: samples.length / 16000, language: language || null };
    },
  };

  process.on('message', async ({ id, method, payload }) => {
    try {
      if (!handlers[method]) throw new Error(`Unknown request ${method}.`);
      process.send({ id, ok: true, result: await handlers[method](payload || {}) });
    } catch (error) {
      process.send({ id, ok: false, error: error?.message || String(error) });
    }
  });
  process.send({ type: 'ready' });
}

// The parent going away ends this process too.
process.on('disconnect', () => process.exit(0));

if (require.main === module) {
  main().catch((error) => {
    process.send?.({ type: 'error', error: error?.message || String(error) });
    process.exit(1);
  });
}

module.exports = { speechParts };
