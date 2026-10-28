'use strict';

exports.up = (db) => {
  db.exec(`
    CREATE TABLE user_two_factor (
      user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
      secret_encrypted TEXT,
      pending_secret_encrypted TEXT,
      enabled INTEGER NOT NULL DEFAULT 0,
      enabled_at TEXT,
      failed_attempts INTEGER NOT NULL DEFAULT 0,
      locked_until INTEGER NOT NULL DEFAULT 0,
      updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
    );

    CREATE TABLE user_recovery_codes (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      code_hash TEXT NOT NULL,
      used_at TEXT
    );
    CREATE INDEX idx_user_recovery_codes_user ON user_recovery_codes(user_id);
  `);
};
