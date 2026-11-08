'use strict';

// Chat, safety and embedding models: one llama.cpp server per model, bound to
// 127.0.0.1 on a free port and locked with a random key, so only NeoRouter
// can reach it.
const crypto = require('node:crypto');
const llamaCpp = require('../runtimes/llama_cpp');
const managed = require('./process');

const LOAD_TIMEOUT_MS = 15 * 60_000;
const POLL_MS = 1000;
// Errors that mean the GPU build cannot work here, so the CPU build should take over.
const GPU_FAILURE = /cuda|cublas|ggml_cuda|no kernel image|driver version is insufficient|metal.*(fail|error)/i;

// How much conversation a model keeps in mind, from the machine's memory
// (more context needs more memory). Embedding models use their own limit.
function contextSize(entry, hardware) {
  if (entry.kind === 'embedding') return 8192;
  const totalGb = hardware.memory.totalMb / 1024;
  return totalGb >= 64 ? 32768 : totalGb >= 24 ? 16384 : 8192;
}

async function get(url, token, timeoutMs = 5000) {
  const response = await fetch(url, { headers: token ? { Authorization: `Bearer ${token}` } : {}, signal: AbortSignal.timeout(timeoutMs) });
  return { status: response.status, data: await response.json().catch(() => null) };
}

async function waitUntilLoaded(proc, origin, signal) {
  const deadline = Date.now() + LOAD_TIMEOUT_MS;
  while (Date.now() < deadline) {
    signal?.throwIfAborted();
    if (proc.exited) throw new Error('stopped while loading');
    try {
      if ((await get(`${origin}/health`)).status === 200) return;
    } catch { /* not listening yet */ }
    await new Promise((resolve) => { setTimeout(resolve, POLL_MS); });
  }
  throw new Error('took too long to load');
}

function support(hardware) {
  return llamaCpp.support(hardware);
}

async function prepare(hardware, options) {
  await llamaCpp.ensure(hardware, options);
}

/**
 * Starts the model and resolves once it answers. files: { model, projector? }
 * (absolute paths). → { proc, endpoint, contextWindow, gpu, health() }
 */
async function start({ entry, files, hardware, signal }) {
  const runtime = await llamaCpp.ensure(hardware, { signal });
  const port = await managed.freePort();
  const token = crypto.randomBytes(24).toString('hex');
  const args = [
    '-m', files.model,
    '--host', '127.0.0.1', '--port', String(port),
    '--api-key', token,
    '--alias', entry.id,
    '--ctx-size', String(contextSize(entry, hardware)),
    '--no-webui',
  ];
  if (files.projector) args.push('--mmproj', files.projector);
  if (entry.kind === 'embedding') args.push('--embeddings');
  const proc = managed.start(runtime.binary, args, { cwd: runtime.dir, env: runtime.env, marker: runtime.binary });
  const origin = `http://127.0.0.1:${port}`;
  try {
    await waitUntilLoaded(proc, origin, signal);
  } catch (error) {
    const output = proc.output();
    await proc.stop();
    // A GPU build that fails on this machine is put aside; the next start uses the CPU build.
    if (runtime.gpu && runtime.id !== 'macos-arm64' && GPU_FAILURE.test(output)) llamaCpp.markBroken(runtime.id);
    throw Object.assign(new Error(`The model ${error.message}.`), { output });
  }
  const props = await get(`${origin}/props`, token).catch(() => null);
  proc.ready();
  return {
    proc,
    endpoint: { baseUrl: `${origin}/v1`, token },
    contextWindow: props?.data?.default_generation_settings?.n_ctx || contextSize(entry, hardware),
    gpu: runtime.gpu,
    health: async () => (await get(`${origin}/health`).catch(() => ({ status: 0 }))).status === 200,
  };
}

module.exports = { id: 'llama_cpp', support, prepare, start, contextSize };
