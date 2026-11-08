'use strict';

// Getting a model onto disk: the engine it needs and its files, resumable,
// checked against their hashes, never more than the free disk space allows.
// One job per model; its progress (with time left) is what the admin sees.
const fs = require('node:fs');
const hardware = require('./hardware');
const fit = require('./fit');
const paths = require('./paths');
const hfFiles = require('./hf_files');
const downloader = require('./downloader');
const engines = require('./engines');
const llamaCpp = require('./runtimes/llama_cpp');
const { createLogger } = require('../../utils/logger');

const logger = createLogger('local-models');

const MB = 1024 * 1024;
// Download speed is measured over this window, for the time left.
const RATE_WINDOW_MS = 10_000;

// id → { controller, phase, doneBytes, totalBytes, samples, error }
const jobs = new Map();

function track(job, delta) {
  job.doneBytes = Math.max(0, job.doneBytes + delta);
  const now = Date.now();
  if (!job.samples.length || now - job.samples.at(-1).at >= 1000) {
    job.samples.push({ at: now, bytes: job.doneBytes });
    while (job.samples.length > 1 && now - job.samples[0].at > RATE_WINDOW_MS) job.samples.shift();
  }
}

// Seconds left at the current speed, or null while that is not known yet.
function secondsLeft(job) {
  if (!['runtime', 'model'].includes(job.phase) || job.samples.length < 3) return null;
  const first = job.samples[0];
  const last = job.samples.at(-1);
  const rate = (last.bytes - first.bytes) / ((last.at - first.at) / 1000);
  return rate > 0 ? Math.ceil((job.totalBytes - job.doneBytes) / rate) : null;
}

function bytesOnDisk(dir, files) {
  return files.reduce((sum, file) => {
    try { return sum + fs.statSync(paths.inside(dir, file.path)).size; } catch { return sum; }
  }, 0);
}

/**
 * Downloads what the entry needs, then calls onInstalled(). verify:
 * also checks the files already there and fetches damaged ones again (repairs).
 */
async function run(entry, { verify = false, onInstalled }) {
  const controller = new AbortController();
  const job = { controller, phase: 'preparing', doneBytes: 0, totalBytes: entry.downloadMb * MB, samples: [], error: null };
  jobs.set(entry.id, job);
  const { signal } = controller;
  const onBytes = (delta) => track(job, delta);
  try {
    const machine = await hardware.detect({ fresh: true });
    const files = await hfFiles.resolve(entry, signal);
    const dir = paths.modelDir(entry.id);
    if (verify) {
      job.phase = 'checking';
      for (const file of files) {
        const target = paths.inside(dir, file.path);
        if (fs.existsSync(target) && !(await downloader.isComplete(target, { ...file, verify: true, signal }))) fs.rmSync(target, { force: true });
      }
    }
    const runtimeBytes = entry.engine === 'llama_cpp' ? await llamaCpp.pendingBytes(machine, signal) : 0;
    const modelBytes = files.reduce((sum, file) => sum + (file.size || 0), 0);
    job.totalBytes = runtimeBytes + modelBytes;
    const missingMb = (runtimeBytes + modelBytes - bytesOnDisk(dir, files)) / MB;
    if (machine.disk.freeMb !== null && missingMb + fit.DISK_MARGIN_MB > machine.disk.freeMb) {
      throw Object.assign(new Error('There is not enough free disk space for this model.'), { code: 'not_enough_disk' });
    }
    job.phase = 'runtime';
    await engines.engineFor(entry.engine).prepare(machine, { onBytes, signal });
    job.phase = 'model';
    for (const file of files) await downloader.download(file, paths.inside(dir, file.path), { onBytes, signal });
    if (jobs.get(entry.id) === job) jobs.delete(entry.id);
    logger.info('Local model installed', { model: entry.id, bytes: modelBytes });
    await onInstalled();
  } catch (error) {
    // A newer job (a repair, a retry) may already have taken this one's place.
    if (signal.aborted) {
      if (jobs.get(entry.id) === job) jobs.delete(entry.id);
      return;
    }
    logger.warn('Local model download failed', { model: entry.id, error: error.message });
    job.error = { code: error.code || 'download_failed', message: error.message };
    job.phase = 'failed';
  }
}

/** Progress for the dashboard, or null when nothing is happening. */
function progress(id) {
  const job = jobs.get(id);
  if (!job) return null;
  return { phase: job.phase, doneBytes: job.doneBytes, totalBytes: job.totalBytes, secondsLeft: secondsLeft(job), error: job.error };
}

const active = (id) => Boolean(jobs.get(id) && !jobs.get(id).error);

function cancel(id) {
  jobs.get(id)?.controller.abort();
  jobs.delete(id);
}

function cancelAll() {
  for (const id of [...jobs.keys()]) cancel(id);
}

module.exports = { run, progress, active, cancel, cancelAll };
