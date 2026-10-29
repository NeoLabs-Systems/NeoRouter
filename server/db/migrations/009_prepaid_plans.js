'use strict';

// Prepaid plans: subscriptions with an included allowance (Ollama Cloud Pro,
// Synthetic). The account is billed, so billing_mode stays 'paid', but each
// request costs nothing extra until the allowance runs out.
exports.up = (db) => {
  db.exec('ALTER TABLE provider_accounts ADD COLUMN prepaid INTEGER NOT NULL DEFAULT 0');
};
