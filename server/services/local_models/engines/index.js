'use strict';

// The engines local models run on. Each exposes:
//   support(hardware)            → { ok, reason }
//   prepare(hardware, options)   installs what it needs (options: onBytes, signal)
//   start({ entry, files, hardware, signal })
//                                → { proc, endpoint? | invoke?, contextWindow, gpu, health() }
const ENGINES = {
  llama_cpp: require('./llama_server'),
  transformers: require('./transformers_engine'),
};

function engineFor(id) {
  const engine = ENGINES[id];
  if (!engine) throw new Error(`Unknown local engine ${id}.`);
  return engine;
}

module.exports = { engineFor };
