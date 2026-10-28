'use strict';

// Learned smallest max_tokens with which a reasoning model still produces an
// answer after its thinking (see routing/learning.js, kind "truncated").
exports.up = (db) => {
  db.exec('ALTER TABLE model_stats ADD COLUMN min_output_tokens INTEGER NOT NULL DEFAULT 0');
};
