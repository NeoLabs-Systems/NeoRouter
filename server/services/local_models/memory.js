'use strict';

// How much memory this server really uses, measured every 30 seconds and
// averaged over the last hour: by everything else (the system, NeoRouter,
// other programs) and by each local model while it runs. The fit check uses
// these averages instead of fixed guesses once they are known.
const fs = require('node:fs');
const os = require('node:os');
const { execFile } = require('node:child_process');

const SAMPLE_EVERY_MS = 30_000;
const WINDOW = 120; // samples: one hour
const MB = 1024 * 1024;

let othersSamples = [];
const modelSamples = new Map(); // id → [mb]
let timer = null;
let sampling = null;
let fixed = false; // tests set the figures instead of measuring
// () => [{ id, pid }] of the models running now; set by the service.
let runningModels = () => [];

function run(command, args) {
  return new Promise((resolve) => {
    execFile(command, args, { timeout: 5000 }, (error, stdout) => resolve(error ? null : String(stdout)));
  });
}

function readFile(file) {
  try { return fs.readFileSync(file, 'utf8'); } catch { return null; }
}

function field(text, name) {
  const match = text?.match(new RegExp(`^${name}:?\\s+(\\d+)`, 'm'));
  return match ? Number(match[1]) : null;
}

// cgroup v2 then v1: the container's own use, without file cache it can drop.
function containerUsedMb() {
  for (const [usage, stat] of [['/sys/fs/cgroup/memory.current', '/sys/fs/cgroup/memory.stat'], ['/sys/fs/cgroup/memory/memory.usage_in_bytes', '/sys/fs/cgroup/memory/memory.stat']]) {
    const current = Number(readFile(usage)?.trim());
    if (!Number.isFinite(current) || current <= 0 || current >= os.totalmem()) continue;
    const stats = readFile(stat);
    const inactive = field(stats, 'inactive_file') ?? field(stats, 'total_inactive_file') ?? 0;
    return { usedMb: (current - inactive) / MB, container: true };
  }
  return null;
}

// macOS: memory in use the way Activity Monitor counts it (app memory, wired,
// compressed). Models on the graphics chip show up as wired memory.
async function macUsedMb() {
  const text = await run('vm_stat', []);
  if (!text) return null;
  const page = Number(text.match(/page size of (\d+) bytes/)?.[1]) || 16384;
  const pages = (label) => Number(text.match(new RegExp(`${label}:\\s+(\\d+)`))?.[1] || 0);
  const used = pages('Anonymous pages') - pages('Pages purgeable') + pages('Pages wired down') + pages('Pages occupied by compressor');
  return used > 0 ? { usedMb: (used * page) / MB } : null;
}

async function systemUsedMb() {
  if (process.platform === 'linux') {
    const container = containerUsedMb();
    if (container) return container;
    const info = readFile('/proc/meminfo');
    const total = field(info, 'MemTotal');
    const available = field(info, 'MemAvailable');
    if (total && available !== null) return { usedMb: (total - available) / 1024 };
  }
  if (process.platform === 'darwin') {
    const mac = await macUsedMb();
    if (mac) return mac;
  }
  return { usedMb: (os.totalmem() - os.freemem()) / MB };
}

// A model process's memory: { totalMb, countedMb }. `countedMb` is the part the
// system figure above includes; on a Linux host a model's weights sit in the
// file cache, which "available" memory already treats as free.
async function processMb(pid, { container }) {
  if (process.platform === 'linux') {
    const status = readFile(`/proc/${pid}/status`);
    const rss = field(status, 'VmRSS');
    if (rss === null) return null;
    const anon = field(status, 'RssAnon') ?? rss;
    return { totalMb: rss / 1024, countedMb: (container ? rss : anon) / 1024 };
  }
  const rss = Number((await run('ps', ['-o', 'rss=', '-p', String(pid)]))?.trim());
  return Number.isFinite(rss) && rss > 0 ? { totalMb: rss / 1024, countedMb: rss / 1024 } : null;
}

function push(list, value) {
  list.push(Math.round(value));
  if (list.length > WINDOW) list.splice(0, list.length - WINDOW);
}

const average = (list) => (list.length ? Math.round(list.reduce((sum, value) => sum + value, 0) / list.length) : null);

async function sample() {
  const system = await systemUsedMb();
  let modelsMb = 0;
  for (const { id, pid } of runningModels()) {
    const measured = pid ? await processMb(pid, { container: Boolean(system.container) }) : null;
    if (!measured) continue;
    modelsMb += measured.countedMb;
    if (!modelSamples.has(id)) modelSamples.set(id, []);
    push(modelSamples.get(id), measured.totalMb);
  }
  push(othersSamples, Math.max(0, system.usedMb - modelsMb));
}

/** Takes a sample now (one at a time), e.g. before deciding whether a model fits. */
function refresh() {
  if (fixed) return Promise.resolve();
  if (!sampling) sampling = sample().catch(() => {}).finally(() => { sampling = null; });
  return sampling;
}

/** Starts measuring; `models` lists the running models as [{ id, pid }]. */
function start(models) {
  runningModels = models;
  if (timer) return;
  refresh();
  timer = setInterval(refresh, SAMPLE_EVERY_MS);
  timer.unref?.();
}

function stop() {
  clearInterval(timer);
  timer = null;
}

/** Average memory used by everything except local models, or null before the first sample. */
function othersMb() {
  return average(othersSamples);
}

/** Average memory a model used while running, or null if it has not run yet. */
function modelMb(id) {
  return average(modelSamples.get(id) || []);
}

function forget(id) {
  modelSamples.delete(id);
}

// Fixed figures instead of measurements; null measures again.
function setForTests(figures) {
  stop();
  fixed = Boolean(figures);
  const { others = null, models = {} } = figures || {};
  othersSamples = others === null ? [] : [others];
  modelSamples.clear();
  for (const [id, mb] of Object.entries(models)) modelSamples.set(id, [mb]);
}

module.exports = { start, stop, refresh, othersMb, modelMb, forget, setForTests, SAMPLE_EVERY_MS };
