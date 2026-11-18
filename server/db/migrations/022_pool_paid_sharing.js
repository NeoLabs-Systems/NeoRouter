'use strict';

// Sharing paid models with a pool is a separate, explicit choice per pool and
// connection: off for everything shared so far.
exports.up = (db) => {
  db.exec('ALTER TABLE pool_shared_accounts ADD COLUMN share_paid INTEGER NOT NULL DEFAULT 0');
};

exports.down = (db) => {
  db.exec('ALTER TABLE pool_shared_accounts DROP COLUMN share_paid');
};
