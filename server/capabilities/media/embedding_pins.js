'use strict';

// Vectors from two embedding models, or one model at two sizes, live in
// different spaces: mixing them silently breaks every similarity search. So
// "auto" embeddings pin a key to the first model (and dimension) it used;
// fallback then only moves between providers serving that same model.
const { getDatabase } = require('../../db/database');

function get(keyId) {
  return getDatabase().prepare('SELECT canonical_id canonicalId, dimensions FROM embedding_pins WHERE router_key_id = ?').get(keyId) || null;
}

function set(keyId, canonicalId, dimensions) {
  getDatabase().prepare(`INSERT INTO embedding_pins (router_key_id, canonical_id, dimensions, created_at) VALUES (?, ?, ?, ?)
    ON CONFLICT(router_key_id) DO NOTHING`).run(keyId, canonicalId, dimensions || null, Date.now());
}

function clear(keyId) {
  return getDatabase().prepare('DELETE FROM embedding_pins WHERE router_key_id = ?').run(keyId).changes > 0;
}

module.exports = { get, set, clear };
