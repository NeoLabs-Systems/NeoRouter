'use strict';

// A limit on the tokens a key uses on free and subscription models, per
// day, month or in total, next to the budget that covers paid models.
exports.up = (db) => {
  db.exec(`ALTER TABLE router_keys ADD COLUMN token_limit INTEGER;
    ALTER TABLE router_keys ADD COLUMN token_period TEXT NOT NULL DEFAULT 'monthly';`);
};
