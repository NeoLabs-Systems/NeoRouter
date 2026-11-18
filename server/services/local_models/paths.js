'use strict';

// Where local models live on disk: the engines NeoRouter downloads
// (runtimes/), the model files (models/<id>/, laid out like the source repo)
// and the processes still running (processes.json, so a crashed NeoRouter
// can end them on the next start).
const fs = require('node:fs');
const path = require('node:path');
const { DATA_DIR } = require('../../../runtime/paths');

const ROOT = path.join(DATA_DIR, 'local-models');
const RUNTIMES_DIR = path.join(ROOT, 'runtimes');
const MODELS_DIR = path.join(ROOT, 'models');
const PROCESSES_FILE = path.join(ROOT, 'processes.json');

function ensureDirs() {
  for (const dir of [ROOT, RUNTIMES_DIR, MODELS_DIR]) fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
}

const modelDir = (id) => path.join(MODELS_DIR, id);

// A path inside `base` from a repo-relative one; refuses anything that would leave it.
function inside(base, relative) {
  const target = path.resolve(base, relative);
  if (target !== base && !target.startsWith(`${base}${path.sep}`)) throw new Error(`Refusing a path outside ${base}: ${relative}`);
  return target;
}

module.exports = { ROOT, RUNTIMES_DIR, MODELS_DIR, PROCESSES_FILE, ensureDirs, modelDir, inside };
