'use strict';

// When a model's output-room floor (min_output_tokens) was last learned, so
// it can be tried again without it now and then (stats_store.outputFloor).
// Floors learned before this have no time and are rechecked right away.
exports.up = (db) => {
  db.exec('ALTER TABLE model_stats ADD COLUMN min_output_at INTEGER NOT NULL DEFAULT 0');
};

exports.down = (db) => {
  db.exec('ALTER TABLE model_stats DROP COLUMN min_output_at');
};
