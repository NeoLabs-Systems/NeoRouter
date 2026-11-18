'use strict';

// Models an admin runs on this server (services/local_models). status is
// where the files are (downloading or installed); enabled is whether the
// admin wants it running. Live state (starting, running, problems) is not
// stored: it is rebuilt from these rows on every start.
exports.up = (db) => {
  db.exec(`
    CREATE TABLE local_models (
      id TEXT PRIMARY KEY,
      status TEXT NOT NULL CHECK (status IN ('downloading', 'installed')),
      enabled INTEGER NOT NULL DEFAULT 1,
      installed_by_user_id TEXT,
      created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
      updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
    );
  `);
};

exports.down = (db) => {
  db.exec('DROP TABLE local_models');
};
