'use strict';

const { getDatabase } = require('../../db/database');
const consent = require('./consent');

// Dashboard only. Keep a bounded in-memory feed independent of usage history.
const MAX_EVENTS = 512;
const events = [];

function record(userId, keyId, { id, status, trail, latencyMs, queuedMs }) {
  events.unshift({ userId, keyId, log: {
    id, evaluator: true, createdAt: Date.now(), status, latencyMs, queuedMs,
    attempts: trail.length,
    trail: trail.map(({ model, outcome }) => ({ model, outcome })),
  } });
  if (events.length > MAX_EVENTS) events.pop();
}

function recent(userId, limit) {
  const disabled = { evaluatorEnabled: false, evaluatorLogs: [] };
  if (!consent.globalSettings().enabled || !consent.userSettings(userId).enabled) return disabled;
  const keys = getDatabase().prepare(`SELECT id, user_id, peer_link_id, created_by_user_id, revoked_at, expires_at
    FROM router_keys WHERE user_id = ? AND benchmark_collection = 1 AND revoked_at IS NULL`).all(userId);
  const activeKeys = new Set(keys.filter(consent.personalKey).map(({ id }) => id));
  if (!activeKeys.size) return disabled;
  return {
    evaluatorEnabled: true,
    evaluatorLogs: events.filter((event) => event.userId === userId && activeKeys.has(event.keyId)).slice(0, limit).map(({ log }) => log),
  };
}

consent.events.on('revoke', ({ userId, keyId }) => {
  for (let i = events.length - 1; i >= 0; i--) {
    const event = events[i];
    if ((!userId || event.userId === userId) && (!keyId || event.keyId === keyId)) events.splice(i, 1);
  }
});

module.exports = { record, recent };
