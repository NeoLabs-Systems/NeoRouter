'use strict';

// Speech providers produce different formats (Gemini WAV, Groq WAV, others
// MP3). When ffmpeg is installed the gateway converts to the format the
// client asked for; without it the original format is returned and named in
// the Content-Type and X-NeoRouter-Audio-Format headers.
const { spawn, spawnSync } = require('node:child_process');

const MIME = { mp3: 'audio/mpeg', opus: 'audio/ogg', aac: 'audio/aac', flac: 'audio/flac', wav: 'audio/wav', pcm: 'audio/pcm' };
const FFMPEG_ARGS = {
  mp3: ['-f', 'mp3', '-c:a', 'libmp3lame', '-b:a', '128k'],
  opus: ['-f', 'ogg', '-c:a', 'libopus', '-b:a', '64k'],
  aac: ['-f', 'adts', '-c:a', 'aac', '-b:a', '128k'],
  flac: ['-f', 'flac'],
  wav: ['-f', 'wav'],
  pcm: ['-f', 's16le', '-ac', '1', '-ar', '24000'],
};
let available = null;

function hasFfmpeg() {
  if (available === null) {
    try { available = spawnSync('ffmpeg', ['-version'], { timeout: 5000 }).status === 0; } catch { available = false; }
  }
  return available;
}

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
  return new Promise((resolve, reject) => {
    const child = spawn('ffmpeg', ['-hide_banner', '-loglevel', 'error', '-i', 'pipe:0', ...FFMPEG_ARGS[target], 'pipe:1']);
    const chunks = [];
    const timer = setTimeout(() => child.kill('SIGKILL'), 30_000);
    child.stdout.on('data', (chunk) => chunks.push(chunk));
    child.on('error', reject);
    child.on('close', (code) => {
      clearTimeout(timer);
      if (code === 0 && chunks.length) resolve(Buffer.concat(chunks));
      else reject(new Error(`ffmpeg exited with ${code}`));
    });
    child.stdin.on('error', () => {});
    child.stdin.end(audio);
  });
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
