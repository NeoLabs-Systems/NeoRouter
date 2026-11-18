'use strict';

const { getConfig } = require('../../config/env');
const scoring = require('./scoring');
const plans = require('./plans');

const SOURCE_TIERS = 5;
function sourceTier(entry, now = Date.now()) {
  if (entry.isFlatRate) return 0;
  if (entry.isPlan) return (plans.pace(entry, now) ?? 0) > 1 ? 1 : 2;
  return entry.isFree ? 3 : 4;
}
function waitMs(entry) {
  return entry.slotWaitMs ?? getConfig().routing.slotQueueWaitMs;
}
function deadline(entry, startedAt) {
  const config = getConfig().routing;
  return startedAt + Math.min(waitMs(entry), Math.max(0, config.totalBudgetMs - config.slotOverflowReserveMs));
}
function busyReason(entry, startedAt, now) {
  if (!entry.isFlatRate) return 'account_busy';
  if (entry.overflowPolicy === 'overflow' || now >= deadline(entry, startedAt)) return 'slot_overflow';
  return 'account_busy';
}
function comparable(candidate, busy) {
  if (candidate.canonicalId && candidate.canonicalId === busy.canonicalId) return true;
  return scoring.trustedIntelligence(candidate) + getConfig().routing.slotComparableGap >= scoring.trustedIntelligence(busy);
}
// Explicit queueing waits up to the account's bound. Auto waits only when
// there is no comparable free alternative or eligible subscription overflow.
function waitUntil(busyEntries, available, startedAt, now, { useAvailablePlan = false } = {}) {
  if (available.some((item) => item.entry.isFlatRate)) return null;
  const planAvailable = useAvailablePlan && available.some((item) => item.entry.isPlan);
  let until = null;
  for (const entry of busyEntries) {
    const untilAt = deadline(entry, startedAt);
    if (entry.overflowPolicy === 'overflow' || now >= untilAt) continue;
    // A routing group can use another included subscription without waiting
    // for its preferred connection. An explicit queue policy still applies.
    if (entry.overflowPolicy !== 'queue' && planAvailable) continue;
    if (entry.overflowPolicy !== 'queue' && available.some((item) => item.entry.isFree && comparable(item.entry, entry))) continue;
    until = until === null ? untilAt : Math.min(until, untilAt);
  }
  return until;
}
module.exports = { SOURCE_TIERS, sourceTier, waitMs, busyReason, waitUntil };
