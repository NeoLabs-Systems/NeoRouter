'use strict';

// Speech and transcription models: transformers.js (already part of
// NeoRouter) in a worker process per model, spoken to over IPC.
const crypto = require('node:crypto');
const path = require('node:path');
const paths = require('../paths');
const managed = require('./process');

const WORKER = path.join(__dirname, 'transformers_worker.js');
const LOAD_TIMEOUT_MS = 5 * 60_000;
const PING_TIMEOUT_MS = 15_000;

function support() {
  return { ok: true, reason: null };
}

async function prepare() {
  // Nothing to install: the library ships with NeoRouter.
}

function client(proc) {
  const pending = new Map();
  proc.child.on('message', (message) => {
    const waiting = message && pending.get(message.id);
    if (!waiting) return;
    pending.delete(message.id);
    clearTimeout(waiting.timer);
    if (message.ok) waiting.resolve(message.result);
    else waiting.reject(new Error(message.error));
  });
  proc.on('exit', () => {
    for (const waiting of pending.values()) {
      clearTimeout(waiting.timer);
      waiting.reject(new Error('The model stopped while working on the request.'));
    }
    pending.clear();
  });
  return function invoke(method, payload, { timeoutMs = 10 * 60_000, signal } = {}) {
    if (proc.exited) return Promise.reject(new Error('The model is not running.'));
    return new Promise((resolve, reject) => {
      const id = crypto.randomUUID();
      const timer = setTimeout(() => {
        pending.delete(id);
        reject(new Error('The model took too long to answer.'));
      }, timeoutMs);
      signal?.addEventListener('abort', () => {
        clearTimeout(timer);
        pending.delete(id);
        reject(signal.reason || new Error('aborted'));
      }, { once: true });
      pending.set(id, { resolve, reject, timer });
      proc.child.send({ id, method, payload });
    });
  };
}

function waitReady(proc, signal) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => done(new Error('took too long to load')), LOAD_TIMEOUT_MS);
    const onMessage = (message) => {
      if (message?.type === 'ready') done();
      if (message?.type === 'error') done(new Error(`could not load: ${message.error}`));
    };
    const onExit = () => done(new Error('stopped while loading'));
    const onAbort = () => done(signal.reason || new Error('aborted'));
    function done(error) {
      clearTimeout(timer);
      proc.child.off('message', onMessage);
      proc.off('exit', onExit);
      signal?.removeEventListener('abort', onAbort);
      if (error) reject(error);
      else resolve();
    }
    proc.child.on('message', onMessage);
    proc.once('exit', onExit);
    signal?.addEventListener('abort', onAbort, { once: true });
  });
}

/** → { proc, invoke(method, payload, options), health() } */
async function start({ entry, signal }) {
  const voices = Object.fromEntries((entry.voices || []).filter((voice) => voice.file).map((voice) => [voice.id, paths.inside(paths.modelDir(entry.id), voice.file)]));
  const config = { modelsDir: paths.MODELS_DIR, id: entry.id, task: entry.transformers.task, dtype: entry.transformers.dtype || null, voices };
  const proc = managed.startNode(WORKER, { env: { ...process.env, NEOROUTER_LOCAL_MODEL: JSON.stringify(config) }, marker: WORKER });
  try {
    await waitReady(proc, signal);
  } catch (error) {
    const output = proc.output();
    await proc.stop();
    throw Object.assign(new Error(`The model ${error.message}.`), { output });
  }
  proc.ready();
  const invoke = client(proc);
  return {
    proc,
    invoke,
    contextWindow: null,
    gpu: false,
    health: () => invoke('ping', {}, { timeoutMs: PING_TIMEOUT_MS }).then(() => true, () => false),
  };
}

module.exports = { id: 'transformers', support, prepare, start };
