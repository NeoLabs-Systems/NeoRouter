'use strict';

const path = require('node:path');
const { fork } = require('node:child_process');
const worker = fork(path.resolve(__dirname, '../../server/services/local_models/engines/transformers_worker.js'), [], {
  env: { ...process.env, NEOROUTER_LOCAL_MODEL: JSON.stringify({ modelsDir: process.env.MODEL_CACHE, id: 'openai/privacy-filter', task: 'token-classification', dtype: 'q4' }) },
  serialization: 'advanced', stdio: ['ignore', 'ignore', 'pipe', 'ipc'],
});
let ok = false;
const timeout = setTimeout(() => { worker.kill('SIGKILL'); process.exitCode = 1; console.error('Worker timeout'); }, 90000);
worker.on('message', (message) => {
  if (message.type === 'ready') worker.send({ id: 'research-ping', method: 'ping' });
  else if (message.type === 'error') { console.error(message.error); worker.kill(); }
  else if (message.id === 'research-ping') { ok = message.ok === true && message.result.ok === true; worker.disconnect(); }
});
worker.on('exit', () => {
  clearTimeout(timeout);
  console.log(JSON.stringify({ runtime: process.versions.bun ? 'bun' : 'node', modelWorkerIpc: ok }));
  if (!ok) process.exitCode = 1;
});
