'use strict';
exports.up = (db) => db.exec(`CREATE TABLE outbound_proxies (
 id TEXT PRIMARY KEY, fingerprint TEXT UNIQUE NOT NULL, endpoint TEXT NOT NULL,
 secret_encrypted TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1
)`);
