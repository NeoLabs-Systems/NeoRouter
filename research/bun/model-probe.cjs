'use strict';

const { execFileSync } = require('node:child_process');
const { performance } = require('node:perf_hooks');
const rss = () => Number(execFileSync('ps', ['-o', 'rss=', '-p', String(process.pid)], { encoding: 'utf8' }).trim()) / 1024;
const emit = (stage, extra = {}) => console.log(JSON.stringify({ stage, runtime: process.versions.bun ? 'bun' : 'node', rssMiB: rss(), ...extra }));
(async () => {
  emit('baseline');
  const at = performance.now();
  const { pipeline, env } = await import('@huggingface/transformers');
  env.cacheDir = process.env.MODEL_CACHE;
  env.allowLocalModels = true;
  env.localModelPath = process.env.MODEL_CACHE;
  env.allowRemoteModels = false;
  emit('library-import', { elapsedMs: performance.now() - at });
  const start = performance.now();
  const pipe = await pipeline('token-classification', 'openai/privacy-filter', { dtype: 'q4', device: 'cpu' });
  emit('model-loaded', { elapsedMs: performance.now() - start });
  const infer = performance.now();
  const spans = await pipe('Alex lives in Berlin.', { aggregation_strategy: 'simple' });
  emit('inference', { elapsedMs: performance.now() - infer, spans: spans.length });
  await pipe.dispose();
  emit('disposed');
})().catch((error) => { emit('error', { name: error.name, message: error.message }); process.exitCode = 1; });
