'use strict';

// Task-aware routing: each model's score per skill (from benchmarks), what
// routing learned per model and kind of task, and the task kind of each logged request.
exports.up = (db) => {
  db.exec(`ALTER TABLE model_metadata ADD COLUMN skills_json TEXT;
    CREATE TABLE model_task_stats (
      model_ref INTEGER NOT NULL REFERENCES models(id) ON DELETE CASCADE,
      task TEXT NOT NULL,
      successes REAL NOT NULL DEFAULT 0,
      failures REAL NOT NULL DEFAULT 0,
      liked REAL NOT NULL DEFAULT 0,
      disliked REAL NOT NULL DEFAULT 0,
      updated_at INTEGER NOT NULL,
      PRIMARY KEY (model_ref, task)
    );
    ALTER TABLE requests ADD COLUMN task TEXT;`);
};
