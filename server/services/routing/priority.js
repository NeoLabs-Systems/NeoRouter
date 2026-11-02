'use strict';

// Who goes first when models are busy. A request that has to wait for capacity
// announces what it waits for, and requests of a lower priority leave those
// models and accounts alone until it has been served. Nothing runs differently
// while there is room, and a request never waits on one of its own level.
const LEVELS = ['low', 'normal', 'high'];

const rank = (level) => Math.max(0, LEVELS.indexOf(level || 'normal'));

// target ("model:<ref>" or "account:<id>") → waiting requests per level.
const waiting = new Map();

function join(targets, level) {
  const index = rank(level);
  for (const target of targets) {
    const counts = waiting.get(target) || new Array(LEVELS.length).fill(0);
    counts[index] += 1;
    waiting.set(target, counts);
  }
  let left = false;
  return () => {
    if (left) return;
    left = true;
    for (const target of targets) {
      const counts = waiting.get(target);
      counts[index] -= 1;
      if (counts.every((count) => count === 0)) waiting.delete(target);
    }
  };
}

const outranked = (target, index) => (waiting.get(target) || []).some((count, level) => level > index && count > 0);

/** True while a request of a higher priority waits for this model or its account. */
function heldBack(entry, level) {
  const index = rank(level);
  return outranked(`model:${entry.ref}`, index) || outranked(`account:${entry.accountId}`, index);
}

function resetForTests() {
  waiting.clear();
}

module.exports = { LEVELS, join, heldBack, resetForTests };
