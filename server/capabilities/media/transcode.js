'use strict';

// Speech providers produce different formats (Gemini WAV, Groq WAV, others
// MP3). When ffmpeg is installed the gateway converts to the format the
// client asked for; without it the original format is returned and named in
// the Content-Type and X-NeoRouter-Audio-Format headers.
const { hasFfmpeg, runFfmpeg } = require('../../utils/ffmpeg');

const MIME = { mp3: 'audio/mpeg', opus: 'audio/ogg', aac: 'audio/aac', flac: 'audio/flac', wav: 'audio/wav', pcm: 'audio/pcm' };
const FFMPEG_ARGS = {
  mp3: ['-f', 'mp3', '-c:a', 'libmp3lame', '-b:a', '128k'],
  opus: ['-f', 'ogg', '-c:a', 'libopus', '-b:a', '64k'],
  aac: ['-f', 'adts', '-c:a', 'aac', '-b:a', '128k'],
  flac: ['-f', 'flac'],
  wav: ['-f', 'wav'],
  pcm: ['-f', 's16le', '-ac', '1', '-ar', '24000'],
};

function formatOf(mime) {
  const value = String(mime || '').toLowerCase();
  if (/mpeg|mp3/.test(value)) return 'mp3';
  if (/wav|wave/.test(value)) return 'wav';
  if (/ogg|opus/.test(value)) return 'opus';
  if (/aac/.test(value)) return 'aac';
  if (/flac/.test(value)) return 'flac';
  if (/pcm|l16/.test(value)) return 'pcm';
  return null;
}

function convert(audio, target) {
  return runFfmpeg(audio, FFMPEG_ARGS[target]);
}

/** Returns { audio, format, mime } in the requested format when possible. */
async function toFormat(audio, mime, target) {
  const source = formatOf(mime);
  if (!target || source === target || !FFMPEG_ARGS[target] || !hasFfmpeg()) {
    return { audio, format: source || 'unknown', mime: mime || 'application/octet-stream' };
  }
  try {
    return { audio: await convert(audio, target), format: target, mime: MIME[target] };
  } catch {
    return { audio, format: source || 'unknown', mime };
  }
}

module.exports = { toFormat, formatOf, hasFfmpeg, MIME };
