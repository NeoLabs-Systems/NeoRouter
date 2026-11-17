'use strict';
// Removes everything the withdrawn local benchmarking feature stored.
exports.up = (db) => {
  db.exec(`
    DROP TABLE IF EXISTS benchmark_samples;
    DROP TABLE IF EXISTS benchmark_contributions;
    DROP TABLE IF EXISTS internal_benchmarks;
    DELETE FROM settings WHERE key = 'local_benchmarks';
  `);
  const columns = db.prepare('PRAGMA table_info(router_keys)').all().map((column) => column.name);
  for (const column of ['benchmark_collection', 'benchmark_generation']) {
    if (columns.includes(column)) db.exec(`ALTER TABLE router_keys DROP COLUMN ${column}`);
  }
  if (db.prepare("SELECT 1 FROM sqlite_master WHERE name = 'user_settings'").get()) {
    db.prepare("DELETE FROM user_settings WHERE key = 'local_benchmarks'").run();
  }
};
