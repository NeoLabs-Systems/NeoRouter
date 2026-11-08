'use strict';

// A first set for an admin who does not know where to start: a text model,
// speech recognition and a voice that run well together on this machine.
// From the catalog's recommendations and the fit; no model is named here.
const catalog = require('./catalog');
const fit = require('./fit');

const KINDS = ['chat', 'transcription', 'speech'];
// The text model may take this share of the memory, so the others fit beside it.
const CHAT_SHARE = 0.6;
const RUNS_WELL = new Set(['great', 'good']);

function pick(kind, machine, roomMb) {
  const candidates = catalog.all()
    .filter((entry) => entry.kind === kind)
    .map((entry) => ({ entry, assessed: fit.assess(entry, machine) }))
    .filter(({ assessed }) => RUNS_WELL.has(assessed.rating) && assessed.diskOk && assessed.memoryMb <= roomMb);
  // Recommended first; among the rest the most capable (largest) that still fits.
  candidates.sort((a, b) => Number(b.entry.recommended) - Number(a.entry.recommended) || b.entry.downloadMb - a.entry.downloadMb);
  return candidates[0] || null;
}

/** [{ id, name, kind, downloadMb, memoryMb }] that fit together, or [] when nothing does. */
function suggest(machine) {
  const budgetMb = machine.memory.totalMb - fit.RESERVE_MB;
  const chosen = [];
  let usedMb = 0;
  for (const kind of KINDS) {
    const room = kind === 'chat' ? budgetMb * CHAT_SHARE : budgetMb - usedMb;
    const found = pick(kind, machine, room);
    if (!found) continue;
    usedMb += found.assessed.memoryMb;
    chosen.push({ id: found.entry.id, name: found.entry.name, kind, downloadMb: found.assessed.downloadMb, memoryMb: found.assessed.memoryMb });
  }
  return chosen;
}

module.exports = { suggest };
