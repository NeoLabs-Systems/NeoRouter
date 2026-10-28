'use strict';

// Verified billing state per provider account: "free" only after an automatic
// check proved the account cannot be charged (see providers/billing_check.js).
exports.up = (db) => {
  db.exec(`ALTER TABLE provider_accounts ADD COLUMN billing_state TEXT NOT NULL DEFAULT 'unknown';
    ALTER TABLE provider_accounts ADD COLUMN billing_message TEXT;
    ALTER TABLE provider_accounts ADD COLUMN billing_checked_at TEXT;`);
};
