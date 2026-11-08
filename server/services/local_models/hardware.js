'use strict';

// What this machine offers local models: processor, memory (the container's
// limit where there is one), graphics and free disk space. Read fresh at most
// every 30 seconds; nothing here changes the machine.
const fs = require('node:fs');
const os = require('node:os');
const { execFile } = require('node:child_process');
const paths = require('./paths');

const CACHE_MS = 30_000;
const MB = 1024 * 1024;
// Share of an Apple Silicon Mac's memory its graphics may use (macOS's default limit).
const UNIFIED_GPU_SHARE = 0.7;
let cached = null;

function run(command, args, timeoutMs = 5000) {
  return new Promise((resolve) => {
    execFile(command, args, { timeout: timeoutMs }, (error, stdout) => resolve(error ? null : String(stdout)));
  });
}

// cgroup v2 then v1; a huge number means "no limit".
function containerMemoryLimit() {
  for (const file of ['/sys/fs/cgroup/memory.max', '/sys/fs/cgroup/memory/memory.limit_in_bytes']) {
    try {
      const value = Number(fs.readFileSync(file, 'utf8').trim());
      if (Number.isFinite(value) && value > 0 && value < os.totalmem()) return value;
    } catch { /* not in a cgroup with a limit */ }
  }
  return null;
}

// NVIDIA cards with their memory, and the newest CUDA the driver supports.
async function nvidia() {
  const list = await run('nvidia-smi', ['--query-gpu=name,memory.total,memory.free', '--format=csv,noheader,nounits']);
  if (!list) return null;
  const cards = list.trim().split('\n').map((line) => line.split(',').map((part) => part.trim()))
    .filter((parts) => parts.length >= 3 && Number(parts[1]) > 0)
    .map(([name, total, free]) => ({ name, memoryMb: Number(total), freeMb: Number(free) }));
  if (!cards.length) return null;
  const header = await run('nvidia-smi', []);
  const cuda = header?.match(/CUDA Version:\s*([\d.]+)/)?.[1] || null;
  return { cards, cuda };
}

async function gpu(totalMb) {
  if (process.platform === 'darwin' && process.arch === 'arm64') {
    return { type: 'metal', name: 'Apple GPU', memoryMb: Math.floor(totalMb * UNIFIED_GPU_SHARE), unified: true, cuda: null };
  }
  if (process.platform === 'linux') {
    const found = await nvidia();
    if (found) {
      const names = [...new Set(found.cards.map((card) => card.name))].join(', ');
      return { type: 'cuda', name: found.cards.length > 1 ? `${found.cards.length}× ${names}` : names, memoryMb: found.cards.reduce((sum, card) => sum + card.memoryMb, 0), unified: false, cuda: found.cuda };
    }
  }
  return null;
}

function disk() {
  try {
    paths.ensureDirs();
    const stats = fs.statfsSync(paths.ROOT);
    return { freeMb: Math.floor((stats.bavail * stats.bsize) / MB), totalMb: Math.floor((stats.blocks * stats.bsize) / MB) };
  } catch {
    return { freeMb: null, totalMb: null };
  }
}

async function read() {
  const limit = containerMemoryLimit();
  const totalMb = Math.floor((limit || os.totalmem()) / MB);
  const cpus = os.cpus();
  return {
    platform: process.platform,
    arch: process.arch,
    cpu: { name: (cpus[0]?.model || 'Unknown processor').replace(/\s+/g, ' ').trim(), cores: cpus.length },
    memory: { totalMb, limited: Boolean(limit) },
    gpu: await gpu(totalMb),
    disk: disk(),
  };
}

async function detect({ fresh = false } = {}) {
  if (!fresh && cached && Date.now() - cached.at < CACHE_MS) return cached.value;
  const value = await read();
  cached = { at: Date.now(), value };
  return value;
}

function resetForTests() {
  cached = null;
}

module.exports = { detect, resetForTests };
