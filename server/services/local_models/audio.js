'use strict';

// Audio for the local speech models: what comes in becomes 16 kHz mono
// samples (what speech recognition reads), what a voice produces becomes a
// WAV file. Without ffmpeg only WAV input can be read.
const { hasFfmpeg, runFfmpeg } = require('../../utils/ffmpeg');
const { parseWav, pcmToWav } = require('../../providers/adapters/wav');

const RATE = 16000;

function fromWav(audio) {
  const wav = parseWav(audio);
  if (!wav || wav.fmt.format !== 1 || wav.fmt.bitsPerSample !== 16) return null;
  const { channels, sampleRate } = wav.fmt;
  const frames = Math.floor(wav.data.length / (2 * channels));
  const ratio = sampleRate / RATE;
  const samples = new Float32Array(Math.floor(frames / ratio));
  for (let index = 0; index < samples.length; index += 1) {
    const frame = Math.min(frames - 1, Math.floor(index * ratio));
    let sum = 0;
    for (let channel = 0; channel < channels; channel += 1) sum += wav.data.readInt16LE((frame * channels + channel) * 2);
    samples[index] = sum / channels / 32768;
  }
  return samples;
}

/** Float32Array of 16 kHz mono samples, or null when the audio cannot be read. */
async function toSamples(audio) {
  if (hasFfmpeg()) {
    try {
      const raw = await runFfmpeg(audio, ['-f', 'f32le', '-ac', '1', '-ar', String(RATE)], { timeoutMs: 120_000 });
      return new Float32Array(raw.buffer.slice(raw.byteOffset, raw.byteOffset + raw.length - (raw.length % 4)));
    } catch { /* not decodable by ffmpeg: try reading it as WAV */ }
  }
  return fromWav(audio);
}

/** 16-bit mono WAV from float samples. */
function toWav(samples, sampleRate) {
  const pcm = Buffer.alloc(samples.length * 2);
  for (let index = 0; index < samples.length; index += 1) {
    const value = Math.max(-1, Math.min(1, samples[index]));
    pcm.writeInt16LE(Math.round(value < 0 ? value * 32768 : value * 32767), index * 2);
  }
  return pcmToWav(pcm, sampleRate);
}

module.exports = { toSamples, toWav };
