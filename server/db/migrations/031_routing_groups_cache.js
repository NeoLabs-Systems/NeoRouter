'use strict';

exports.up = (db) => db.exec(`
  CREATE TABLE routing_groups (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    config_json TEXT NOT NULL,
    UNIQUE(user_id, name)
  );
  ALTER TABLE router_keys ADD COLUMN routing_group TEXT;
  ALTER TABLE requests ADD COLUMN routing_group TEXT;
  ALTER TABLE requests ADD COLUMN cached_input_tokens INTEGER NOT NULL DEFAULT 0;
  ALTER TABLE requests ADD COLUMN cache_creation_tokens INTEGER NOT NULL DEFAULT 0;
`);
