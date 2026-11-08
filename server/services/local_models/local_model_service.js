'use strict';

// Local models, end to end: what this machine can run, installing
// (installer.js), running and stopping (one runner per model), repairing, and
// handing requests to the running models. Admins manage it in the dashboard;
// the router reaches it through the "NeoRouter Local" connection
// (providers/adapters/local_runtime.js).
const fs = require('node:fs');
const catalog = require('./catalog');
const hardware = require('./hardware');
const fit = require('./fit');
const paths = require('./paths');
const engines = require('./engines');
const installer = require('./installer');
const starter = require('./starter');
const { endOrphans } = require('./engines/process');
const llamaCpp = require('./runtimes/llama_cpp');
const { Runner } = require('./runner');
const store = require('./store');
const access = require('./access');
const { HttpError } = require('../../middleware/error_handler');
const { createLogger } = require('../../utils/logger');

const logger = createLogger('local-models');

// A model that keeps failing is repaired automatically at most this often.
const AUTO_REPAIR_EVERY_MS = 60 * 60_000;

const runners = new Map();
const autoRepairedAt = new Map();

function entryOrThrow(id) {
  const entry = catalog.get(id);
  if (!entry) throw new HttpError(404, 'LOCAL_MODEL_UNKNOWN', 'This model is not in the catalog.');
  return entry;
}

function rowOrThrow(id) {
  const row = store.get(id);
  if (!row) throw new HttpError(404, 'LOCAL_MODEL_NOT_INSTALLED', 'This model is not installed.');
  return row;
}

// Absolute paths of the files an engine loads.
function filesOf(entry) {
  const dir = paths.modelDir(entry.id);
  return {
    model: entry.engine === 'llama_cpp' ? paths.inside(dir, entry.source.include[0]) : dir,
    projector: entry.source.projector ? paths.inside(dir, entry.source.projector) : null,
  };
}

// ------------------------------------------------------------ running

// Memory taken by models that are running or about to.
function memoryInUse(exceptId = null) {
  let used = 0;
  for (const [id, runner] of runners) {
    if (id !== exceptId && ['starting', 'running', 'restarting'].includes(runner.state)) used += fit.needs(catalog.get(id)).memoryMb;
  }
  return used;
}

function runnerFor(id) {
  if (!runners.has(id)) {
    const runner = new Runner(id, async () => {
      const entry = entryOrThrow(id);
      return { entry, files: filesOf(entry), hardware: await hardware.detect() };
    });
    runner.on('change', () => access.scheduleSync());
    runner.on('gave-up', () => autoRepair(id));
    runners.set(id, runner);
  }
  return runners.get(id);
}

// Starts a model if there is memory for it beside the ones already running.
async function startModel(id) {
  const entry = entryOrThrow(id);
  const runner = runnerFor(id);
  const assessed = fit.assess(entry, await hardware.detect(), { usedMb: memoryInUse(id) });
  if (!assessed.fitsNow) {
    runner.refuse({ code: 'not_enough_memory', message: 'Not enough free memory. Stop another model first.' });
    return;
  }
  runner.start();
}

function download(id, { verify = false } = {}) {
  installer.run(entryOrThrow(id), {
    verify,
    onInstalled: async () => {
      store.update(id, { status: 'installed' });
      if (store.get(id)?.enabled) await startModel(id);
    },
  });
}

// ------------------------------------------------------------ actions

async function install(userId, ids) {
  const machine = await hardware.detect({ fresh: true });
  const entries = ids.map(entryOrThrow);
  for (const entry of entries) {
    if (store.get(entry.id)) throw new HttpError(409, 'LOCAL_MODEL_INSTALLED', 'This model is already installed.');
    const assessed = fit.assess(entry, machine);
    if (assessed.rating === 'unsupported') throw new HttpError(422, 'LOCAL_MODEL_UNSUPPORTED', 'This system cannot run this model.');
    if (assessed.rating === 'too_big') throw new HttpError(422, 'LOCAL_MODEL_TOO_BIG', 'This server does not have enough memory for this model.');
    if (!assessed.diskOk) throw new HttpError(422, 'LOCAL_MODEL_NO_DISK', 'There is not enough free disk space for this model.');
  }
  paths.ensureDirs();
  for (const entry of entries) store.add(entry.id, userId);
  // The admin who installs a model can use it straight away.
  await access.connect(userId);
  for (const entry of entries) download(entry.id);
  return overview();
}

// The suggested first set for this machine, all at once.
async function installStarter(userId) {
  const ids = starter.suggest(await hardware.detect({ fresh: true })).map((item) => item.id).filter((id) => !store.get(id));
  if (!ids.length) throw new HttpError(422, 'LOCAL_MODEL_NO_STARTER', 'No starter set fits this server.');
  return install(userId, ids);
}

// Tries again: a stopped download where it stopped, a model that could not start once more.
async function retry(id) {
  const row = rowOrThrow(id);
  if (installer.active(id)) return overview();
  if (row.status === 'downloading') download(id);
  else {
    store.update(id, { enabled: true });
    await startModel(id);
  }
  return overview();
}

async function setEnabled(id, enabled) {
  const row = rowOrThrow(id);
  store.update(id, { enabled });
  if (row.status !== 'installed') return overview();
  if (enabled) await startModel(id);
  else await runnerFor(id).stop();
  return overview();
}

async function remove(id) {
  rowOrThrow(id);
  installer.cancel(id);
  await runners.get(id)?.stop();
  runners.delete(id);
  store.remove(id);
  fs.rmSync(paths.modelDir(id), { recursive: true, force: true });
  access.scheduleSync();
  return overview();
}

/**
 * Puts a model right: stops it, gives every engine build another chance,
 * checks each file and fetches what is missing or damaged, then starts it.
 */
async function repair(id) {
  rowOrThrow(id);
  installer.cancel(id);
  await runners.get(id)?.stop();
  llamaCpp.clearBroken();
  store.update(id, { enabled: true });
  download(id, { verify: true });
  return overview();
}

function autoRepair(id) {
  const last = autoRepairedAt.get(id) || 0;
  if (Date.now() - last < AUTO_REPAIR_EVERY_MS) return;
  autoRepairedAt.set(id, Date.now());
  logger.warn('Local model keeps failing; repairing it', { model: id });
  repair(id).catch((error) => logger.warn('Automatic repair failed', { model: id, error: error.message }));
}

async function setAccess(level) {
  access.setLevel(level);
  return overview();
}

// ------------------------------------------------------------ for the router

/** The models answering right now, with what the router needs to list them. */
function servedModels() {
  const served = [];
  for (const [id, runner] of runners) {
    const entry = catalog.get(id);
    if (entry && runner.state === 'running' && runner.instance) served.push({ entry, contextWindow: runner.instance.contextWindow });
  }
  return served;
}

function runningInstance(id) {
  const runner = runners.get(id);
  if (!runner?.instance || runner.state !== 'running') return null;
  return runner.instance;
}

// ------------------------------------------------------------ overview

function installedView(row) {
  const entry = catalog.get(row.id);
  const runner = runners.get(row.id);
  const progress = installer.progress(row.id);
  const state = progress ? (progress.error ? 'download_failed' : 'downloading')
    : row.status === 'downloading' ? 'download_failed' : runner?.state || 'stopped';
  return {
    id: row.id,
    name: entry?.name || row.id,
    kind: entry?.kind || null,
    state,
    enabled: row.enabled,
    progress,
    problem: progress?.error || runner?.problem || null,
    memoryMb: entry ? fit.needs(entry).memoryMb : null,
    gpu: runner?.instance ? runner.instance.gpu : null,
  };
}

async function overview() {
  const machine = await hardware.detect();
  const installed = store.list();
  const ids = new Set(installed.map((row) => row.id));
  const usedMb = memoryInUse();
  return {
    system: {
      cpu: machine.cpu,
      memoryMb: machine.memory.totalMb,
      memoryLimited: machine.memory.limited,
      gpu: machine.gpu ? { type: machine.gpu.type, name: machine.gpu.name, memoryMb: machine.gpu.memoryMb } : null,
      disk: machine.disk,
    },
    engines: {
      llama_cpp: { ...engines.engineFor('llama_cpp').support(machine), ...llamaCpp.status(machine) },
      transformers: engines.engineFor('transformers').support(machine),
    },
    memory: { usedMb, budgetMb: Math.max(0, machine.memory.totalMb - fit.RESERVE_MB) },
    access: access.level(),
    starter: installed.length ? [] : starter.suggest(machine),
    installed: installed.map(installedView),
    catalog: catalog.all().map((entry) => ({
      id: entry.id,
      name: entry.name,
      kind: entry.kind,
      summary: entry.summary,
      languages: entry.languages,
      license: entry.license,
      recommended: entry.recommended,
      features: entry.features,
      installed: ids.has(entry.id),
      ...fit.assess(entry, machine, { usedMb }),
    })),
  };
}

// ------------------------------------------------------------ lifecycle

/**
 * On server start: ends model processes a crash left behind, finishes
 * interrupted downloads and starts every model the admin left on, one after
 * another so they do not all load at once.
 */
async function resume() {
  try {
    endOrphans();
    const rows = store.list().filter((row) => catalog.get(row.id));
    for (const row of rows.filter((item) => item.status === 'downloading')) download(row.id);
    for (const row of rows.filter((item) => item.status === 'installed' && item.enabled)) {
      await startModel(row.id);
      await new Promise((resolve) => {
        const runner = runners.get(row.id);
        if (!runner || runner.state !== 'starting') return resolve();
        const done = () => {
          if (runner.state === 'starting') return;
          runner.off('change', done);
          resolve();
        };
        runner.on('change', done);
      });
    }
    access.scheduleSync();
  } catch (error) {
    logger.error('Local models could not be resumed', { error: error.message });
  }
}

async function shutdown() {
  installer.cancelAll();
  await Promise.all([...runners.values()].map((runner) => runner.stop()));
}

async function resetForTests() {
  await shutdown();
  runners.clear();
  autoRepairedAt.clear();
}

module.exports = {
  overview, install, installStarter, retry, setEnabled, remove, repair, setAccess,
  servedModels, runningInstance, resume, shutdown, resetForTests,
};
