'use strict';

// Remove replay evidence and per-key consent from databases that ran 040.
exports.up = (db) => db.exec(`
  DROP TABLE IF EXISTS benchmark_evidence;
  DELETE FROM settings WHERE key = 'local_benchmarks';
  DELETE FROM user_settings WHERE key = 'local_benchmarks';
  ALTER TABLE router_keys DROP COLUMN benchmark_collection;
  ALTER TABLE router_keys DROP COLUMN benchmark_revision;
`);
