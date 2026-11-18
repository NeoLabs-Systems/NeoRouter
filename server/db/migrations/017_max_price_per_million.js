'use strict';

// Maximum price per million tokens for paid models used with this key.
// When set, models whose output price exceeds this limit are rejected.
exports.up = (db) => {
  db.exec(`ALTER TABLE router_keys ADD COLUMN max_price_per_million REAL;`);
};

exports.down = (db) => {
  db.exec(`ALTER TABLE router_keys DROP COLUMN max_price_per_million;`);
};
