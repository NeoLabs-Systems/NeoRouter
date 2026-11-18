'use strict';

module.exports.up = (db) => db.exec(
  'ALTER TABLE router_keys ADD COLUMN concurrent_limit INTEGER CHECK (concurrent_limit > 0)',
);
