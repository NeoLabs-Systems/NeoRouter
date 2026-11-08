'use strict';

// ffmpeg, when the server has it: one place that checks for it and runs it
// on a buffer (audio in on stdin, the result from stdout).
const { spawn, spawnSync } = require('node:child_process');

let available = null;

function hasFfmpeg() {
  if (available === null) {
    try { available = spawnSync('ffmpeg', ['-version'], { timeout: 5000 }).status === 0; } catch { available = false; }
  }
  return available;
}

/** Runs ffmpeg with `args` (between the input and pipe:1) on `input`; resolves with its output. */
function runFfmpeg(input, args, { timeoutMs = 30_000 } = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn('ffmpeg', ['-hide_banner', '-loglevel', 'error', '-i', 'pipe:0', ...args, 'pipe:1']);
    const chunks = [];
    const timer = setTimeout(() => child.kill('SIGKILL'), timeoutMs);
    child.stdout.on('data', (chunk) => chunks.push(chunk));
    child.on('error', reject);
    child.on('close', (code) => {
      clearTimeout(timer);
      if (code === 0 && chunks.length) resolve(Buffer.concat(chunks));
      else reject(new Error(`ffmpeg exited with ${code}`));
    });
    child.stdin.on('error', () => {});
    child.stdin.end(input);
  });
}

module.exports = { hasFfmpeg, runFfmpeg };
