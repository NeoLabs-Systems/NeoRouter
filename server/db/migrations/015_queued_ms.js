'use strict';

// How long a request waited for a model to free up before its first attempt
// (every candidate was rate-limited or busy at that moment).
exports.up = (db) => {
  db.exec('ALTER TABLE requests ADD COLUMN queued_ms INTEGER NOT NULL DEFAULT 0;');
};
