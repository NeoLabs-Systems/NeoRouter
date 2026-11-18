'use strict';

// Limits a person states for one provider account: requests and tokens per
// minute and day, and requests in flight at once. They outrank every
// documented, header-reported or learned value.
exports.up = (db) => {
  db.exec(`
    CREATE TABLE account_limits (
      account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
      metric TEXT NOT NULL CHECK (metric IN ('rpm', 'rpd', 'tpm', 'tpd', 'concurrent')),
      value INTEGER NOT NULL CHECK (value >= 1),
      updated_at INTEGER NOT NULL,
      PRIMARY KEY (account_id, metric)
    );
  `);
};

exports.down = (db) => {
  db.exec('DROP TABLE account_limits;');
};
