'use strict';

// Cost per kind. Token-priced kinds (embeddings, System One) use the per-1M
// token prices; media kinds use a unit price: USD per image, per second of
// video, per minute of audio input, per million characters of speech or per
// web search. An
// unknown price is never free: budget keys skip such models.

function tokenCost(entry, tokens) {
  if (entry.isFree) return 0;
  if (entry.priceIn === null || entry.priceIn === undefined) return null;
  return (tokens * entry.priceIn) / 1e6;
}

function unitCost(entry, unit, amount) {
  if (entry.isFree) return 0;
  if (entry.priceUnit !== unit || entry.unitPrice === null || entry.unitPrice === undefined) return null;
  return (unit === 'mchar' ? amount / 1e6 : amount) * entry.unitPrice;
}

// Rough audio length from the upload size (128 kbit/s ≈ 1 MB per minute),
// used only for the worst-case budget reservation.
function estimateAudioMinutes(bytes) {
  return Math.max(0.1, bytes / 960_000);
}

module.exports = { tokenCost, unitCost, estimateAudioMinutes };
