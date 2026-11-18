'use strict';

exports.up = (db) => db.exec(`
  ALTER TABLE router_keys ADD COLUMN benchmark_collection INTEGER NOT NULL DEFAULT 0;
  ALTER TABLE router_keys ADD COLUMN benchmark_revision TEXT NOT NULL DEFAULT '';
  CREATE TABLE benchmark_evidence (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    canonical_id TEXT NOT NULL,
    task TEXT NOT NULL,
    evaluator TEXT NOT NULL,
    version TEXT NOT NULL,
    bucket INTEGER NOT NULL,
    samples INTEGER NOT NULL,
    confidence_sum REAL NOT NULL,
    score_sum REAL NOT NULL,
    PRIMARY KEY (user_id, canonical_id, task, evaluator, version, bucket)
  );
  CREATE INDEX benchmark_evidence_expiry ON benchmark_evidence(bucket);
`);
