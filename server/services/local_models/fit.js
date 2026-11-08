'use strict';

// Whether a catalog model can run on this machine, and how well, in words an
// admin can act on. Memory is estimated from the download size (weights plus
// room for the conversation), which is close enough to decide "fits or not".
const engines = require('./engines');

// Kept free for the system and NeoRouter itself.
const RESERVE_MB = 2048;
// Room for the conversation (context) and working buffers, per engine.
const OVERHEAD_MB = { llama_cpp: 1024, transformers: 384 };
const WEIGHT_FACTOR = 1.15;
// Spare disk space a download must leave.
const DISK_MARGIN_MB = 1024;
// Without a graphics card, models up to this size still answer at reading speed.
const CPU_COMFORT_MB = 4500;
const SMALL_MB = 1500;

function needs(entry) {
  return {
    downloadMb: entry.downloadMb,
    memoryMb: entry.memoryMb || Math.round(entry.downloadMb * WEIGHT_FACTOR + OVERHEAD_MB[entry.engine]),
  };
}

// great: runs fully on the graphics card (or is small); good: fine on the
// processor; slow: runs, but answers slowly; too_big: not enough memory;
// unsupported: this system cannot run the engine.
function speed(entry, hardware, memoryMb) {
  if (entry.engine === 'transformers') return memoryMb <= SMALL_MB ? 'great' : 'good';
  const gpu = hardware.gpu;
  if (gpu && memoryMb <= gpu.memoryMb) return 'great';
  if (gpu?.type === 'cuda' && memoryMb <= gpu.memoryMb * 1.5) return 'good';
  if (memoryMb <= SMALL_MB) return 'great';
  return memoryMb <= CPU_COMFORT_MB ? 'good' : 'slow';
}

/**
 * { rating, memoryMb, downloadMb, fitsNow, diskOk, reason }
 * usedMb: memory already taken by local models that are running.
 */
function assess(entry, hardware, { usedMb = 0 } = {}) {
  const { memoryMb, downloadMb } = needs(entry);
  const budgetMb = hardware.memory.totalMb - RESERVE_MB;
  const engine = engines.engineFor(entry.engine).support(hardware);
  const diskOk = hardware.disk.freeMb === null || downloadMb + DISK_MARGIN_MB <= hardware.disk.freeMb;
  const base = { memoryMb, downloadMb, diskOk, fitsNow: memoryMb <= budgetMb - usedMb };
  if (!engine.ok) return { ...base, rating: 'unsupported', fitsNow: false, reason: engine.reason };
  if (memoryMb > budgetMb) return { ...base, rating: 'too_big', fitsNow: false, reason: 'not_enough_memory' };
  return { ...base, rating: speed(entry, hardware, memoryMb), reason: null };
}

module.exports = { assess, needs, RESERVE_MB, DISK_MARGIN_MB };
