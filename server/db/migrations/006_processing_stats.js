'use strict';

// Per request: how many values the privacy shield hid and how many tokens
// compression saved (estimated, chars / 4). Counts only, never content.
exports.up = (db) => {
  db.exec(`ALTER TABLE requests ADD COLUMN redacted_count INTEGER NOT NULL DEFAULT 0;
    ALTER TABLE requests ADD COLUMN compression_saved_tokens INTEGER NOT NULL DEFAULT 0;`);
};
