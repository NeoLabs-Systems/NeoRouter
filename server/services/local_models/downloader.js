'use strict';

// Downloads one large file safely: into "<file>.part", resuming where an
// earlier attempt stopped, checked against its size and SHA-256, and only
// then renamed into place. A file already in place with the right size is
// kept (verify: true checks its hash too, for repairs).
const crypto = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');
const { Readable } = require('node:stream');
const { pipeline } = require('node:stream/promises');
const { Transform } = require('node:stream');

const ATTEMPTS = 4;
const RETRY_MS = [2000, 5000, 15000];
// A connection that delivers nothing for this long is dropped and resumed.
const STALL_MS = 60_000;

const anySignal = (...signals) => AbortSignal.any(signals.filter(Boolean));

function sizeOf(file) {
  try { return fs.statSync(file).size; } catch { return null; }
}

async function hashFile(file, signal) {
  const hash = crypto.createHash('sha256');
  await pipeline(fs.createReadStream(file), new Transform({ transform(chunk, _encoding, done) { hash.update(chunk); done(); } }), { signal });
  return hash.digest('hex');
}

/** True when the file is complete (and, with verify, its hash matches). */
async function isComplete(file, { size, sha256, verify = false, signal } = {}) {
  const actual = sizeOf(file);
  if (actual === null || (size !== null && size !== undefined && actual !== size)) return false;
  if (!verify || !sha256) return true;
  return (await hashFile(file, signal)) === sha256;
}

async function attempt({ url, part, size, onBytes, signal }) {
  const offset = sizeOf(part) || 0;
  if (size && offset === size) return;
  const stall = new AbortController();
  let timer = setTimeout(() => stall.abort(), STALL_MS);
  const response = await fetch(url, {
    headers: { 'User-Agent': 'NeoRouter', ...(offset ? { Range: `bytes=${offset}-` } : {}) },
    signal: anySignal(signal, stall.signal),
    redirect: 'follow',
  });
  if (!response.ok || !response.body) {
    clearTimeout(timer);
    const error = new Error(`Download failed with status ${response.status}.`);
    error.retry = response.status >= 500 || response.status === 429;
    throw error;
  }
  // A server that ignores the range starts over from the first byte.
  const resumed = offset > 0 && response.status === 206;
  if (!resumed && offset) onBytes(-offset);
  const counter = new Transform({
    transform(chunk, _encoding, done) {
      clearTimeout(timer);
      timer = setTimeout(() => stall.abort(), STALL_MS);
      onBytes(chunk.length);
      done(null, chunk);
    },
  });
  try {
    await pipeline(Readable.fromWeb(response.body), counter, fs.createWriteStream(part, { flags: resumed ? 'a' : 'w' }), { signal: anySignal(signal, stall.signal) });
  } finally {
    clearTimeout(timer);
  }
}

/**
 * file: { url, size, sha256 }; dest: final path. onBytes(delta) reports
 * progress. Throws when aborted or when the file stays broken.
 */
async function download(file, dest, { onBytes = () => {}, signal } = {}) {
  fs.mkdirSync(path.dirname(dest), { recursive: true });
  if (await isComplete(dest, { size: file.size })) {
    onBytes(file.size || sizeOf(dest) || 0);
    return;
  }
  const part = `${dest}.part`;
  onBytes(sizeOf(part) || 0);
  let lastError = null;
  for (let index = 0; index < ATTEMPTS; index += 1) {
    signal?.throwIfAborted();
    try {
      await attempt({ url: file.url, part, size: file.size, onBytes, signal });
      if (file.size && sizeOf(part) !== file.size) throw Object.assign(new Error('The download ended early.'), { retry: true });
      if (file.sha256 && await hashFile(part, signal) !== file.sha256) {
        onBytes(-(sizeOf(part) || 0));
        fs.rmSync(part, { force: true });
        throw Object.assign(new Error('The downloaded file was damaged.'), { retry: true });
      }
      fs.renameSync(part, dest);
      return;
    } catch (error) {
      if (signal?.aborted) throw error;
      lastError = error;
      const retryable = error.retry !== false;
      if (!retryable || index === ATTEMPTS - 1) break;
      await new Promise((resolve) => { setTimeout(resolve, RETRY_MS[Math.min(index, RETRY_MS.length - 1)]); });
    }
  }
  throw lastError;
}

module.exports = { download, isComplete, hashFile };
