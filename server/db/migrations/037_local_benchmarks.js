'use strict';
exports.up = (db) => db.exec(`
  ALTER TABLE router_keys ADD COLUMN benchmark_collection INTEGER NOT NULL DEFAULT 0;
  ALTER TABLE router_keys ADD COLUMN benchmark_generation TEXT NOT NULL DEFAULT '';
  CREATE TABLE benchmark_samples (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key_id TEXT NOT NULL REFERENCES router_keys(id) ON DELETE CASCADE,
    fingerprint TEXT NOT NULL,
    consent_json TEXT NOT NULL,
    payload TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    calls INTEGER NOT NULL DEFAULT 0,
    next_run_at INTEGER NOT NULL,
    lease_until INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    code TEXT,
    UNIQUE(user_id, fingerprint)
  );
  CREATE INDEX benchmark_work ON benchmark_samples(status, next_run_at, lease_until);
  CREATE INDEX benchmark_expiry ON benchmark_samples(expires_at);
  CREATE INDEX benchmark_owner ON benchmark_samples(user_id, expires_at);
  CREATE TABLE benchmark_contributions (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    fingerprint TEXT NOT NULL,
    canonical_id TEXT NOT NULL,
    task TEXT NOT NULL,
    score REAL NOT NULL,
    confidence REAL NOT NULL,
    evaluated_at INTEGER NOT NULL,
    evaluator_version TEXT NOT NULL,
    judges_json TEXT NOT NULL,
    PRIMARY KEY(user_id, fingerprint, canonical_id, evaluator_version)
  );
  CREATE INDEX benchmark_evidence_group ON benchmark_contributions(canonical_id, task, evaluator_version, evaluated_at DESC);
  CREATE INDEX benchmark_evidence_expiry ON benchmark_contributions(evaluated_at);
  CREATE TABLE internal_benchmarks (
    canonical_id TEXT NOT NULL,
    task TEXT NOT NULL,
    score REAL NOT NULL,
    samples INTEGER NOT NULL,
    effective_samples REAL NOT NULL,
    updated_at INTEGER NOT NULL,
    confidence REAL NOT NULL,
    evaluated_at INTEGER NOT NULL,
    evaluator_version TEXT NOT NULL,
    judges_json TEXT NOT NULL,
    PRIMARY KEY(canonical_id, task, evaluator_version)
  );
`);
