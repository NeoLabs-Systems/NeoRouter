'use strict';

// What last happened with a key, for the "waiting for your app" check:
// whether a request arrived, and if it failed before it reached a model
// (an unknown model name, a bad request), why. Kept in memory only, one
// entry per key, never the prompt.
const { getDatabase } = require('../../db/database');

const failures = new Map();
const MAX_KEYS = 2000;

/** Called by the gateway's error handler for requests with a valid key. */
function recordFailure(keyId, { status, code, message, model }) {
  if (!keyId) return;
  failures.delete(keyId);
  failures.set(keyId, { at: Date.now(), status, code: code || null, message: String(message || '').slice(0, 300), model: model ? String(model).slice(0, 200) : null });
  if (failures.size > MAX_KEYS) failures.delete(failures.keys().next().value);
}

/**
 * { lastUsedAt, request: { at, ok, model, errorCode, errorMessage, latencyMs } | null }
 * for a key of this owner, or null when there is no such key. Times in ms.
 */
function activity(ownerId, keyId) {
  const db = getDatabase();
  const key = db.prepare('SELECT id, last_used_at FROM router_keys WHERE id = ? AND user_id = ?').get(keyId, ownerId);
  if (!key) return null;
  const row = db.prepare(`SELECT created_at, status, model_label, requested_model, error_code, error_message, latency_ms
    FROM requests WHERE router_key_id = ? ORDER BY created_at DESC LIMIT 1`).get(keyId);
  let request = row ? {
    at: row.created_at, ok: row.status === 'success', model: row.model_label || row.requested_model,
    requested: row.requested_model, errorCode: row.error_code, errorMessage: row.error_message, latencyMs: row.latency_ms,
  } : null;
  const failure = failures.get(keyId);
  if (failure && (!request || failure.at > request.at)) {
    request = { at: failure.at, ok: false, model: null, requested: failure.model, errorCode: failure.code, errorMessage: failure.message, latencyMs: null };
  }
  return { lastUsedAt: key.last_used_at ? Date.parse(key.last_used_at) : null, request };
}

function resetForTests() {
  failures.clear();
}

module.exports = { recordFailure, activity, resetForTests };
