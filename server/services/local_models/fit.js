'use strict';

// Whether a catalog model can run on this machine, and how well, in words an
// admin can act on. A model's memory is what it used on average while it ran
// here, or until then an estimate from the download size (weights plus room
// for the conversation). The room left for models is the machine's memory
// minus what everything else uses on average (memory.js), never less than a
// fixed reserve.
const engines = require('./engines');

// Always kept free for the system and NeoRouter itself.
const RESERVE_MB = 2048;
// Spare room on top of what everything else uses on average, for its peaks.
const HEADROOM_MB = 1024;
// Room for the conversation (context) and working buffers, per engine.
const OVERHEAD_MB = { llama_cpp: 1024, transformers: 384 };
const WEIGHT_FACTOR = 1.15;
// Spare disk space a download must leave.
const DISK_MARGIN_MB = 1024;
// Without a graphics card, models up to this size still answer at reading speed.
const CPU_COMFORT_MB = 4500;
const SMALL_MB = 1500;

/** measuredMb: the model's average while it ran here, if known. */
function needs(entry, measuredMb = null) {
  return {
    downloadMb: entry.downloadMb,
    memoryMb: measuredMb || entry.memoryMb || Math.round(entry.downloadMb * WEIGHT_FACTOR + OVERHEAD_MB[entry.engine]),
    measured: Boolean(measuredMb),
  };
}

// The most memory models could ever have here (everything else at a minimum).
const capacityMb = (hardware) => Math.max(0, hardware.memory.totalMb - RESERVE_MB);

/** Memory models can use beside what everything else uses on average. */
function roomMb(hardware) {
  const others = hardware.memory.othersMb;
  const kept = others == null ? RESERVE_MB : Math.max(RESERVE_MB, others + HEADROOM_MB);
  return Math.max(0, hardware.memory.totalMb - kept);
}

// great: runs fully on the graphics card (or is small); good: fine on the
// processor; slow: runs, but answers slowly; tight: fits the machine, but not
// beside what it normally uses for other things; too_big: not enough memory;
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
 * { rating, memoryMb, measured, downloadMb, fitsNow, diskOk, reason }
 * usedMb: memory already taken by local models that are running.
 * measuredMb: what this model used on average while it ran here.
 */
function assess(entry, hardware, { usedMb = 0, measuredMb = null } = {}) {
  const { memoryMb, measured, downloadMb } = needs(entry, measuredMb);
  const room = roomMb(hardware);
  const engine = engines.engineFor(entry.engine).support(hardware);
  const diskOk = hardware.disk.freeMb === null || downloadMb + DISK_MARGIN_MB <= hardware.disk.freeMb;
  const base = { memoryMb, measured, downloadMb, diskOk, fitsNow: memoryMb <= room - usedMb };
  if (!engine.ok) return { ...base, rating: 'unsupported', fitsNow: false, reason: engine.reason };
  if (memoryMb > capacityMb(hardware)) return { ...base, rating: 'too_big', fitsNow: false, reason: 'not_enough_memory' };
  if (memoryMb > room) return { ...base, rating: 'tight', reason: 'memory_in_use' };
  return { ...base, rating: speed(entry, hardware, memoryMb), reason: null };
}

module.exports = { assess, needs, roomMb, RESERVE_MB, HEADROOM_MB, DISK_MARGIN_MB };
