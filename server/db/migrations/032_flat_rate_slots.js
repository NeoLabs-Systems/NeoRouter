'use strict';

exports.up = (db) => db.exec(`
  ALTER TABLE provider_accounts ADD COLUMN overflow_policy TEXT NOT NULL DEFAULT 'auto' CHECK(overflow_policy IN ('auto', 'overflow', 'queue'));
  ALTER TABLE provider_accounts ADD COLUMN slot_wait_ms INTEGER CHECK(slot_wait_ms IS NULL OR slot_wait_ms >= 0);
  CREATE TABLE slot_utilization (
    account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
    bucket INTEGER NOT NULL,
    observed_ms INTEGER NOT NULL,
    busy_ms INTEGER NOT NULL,
    capacity_ms INTEGER NOT NULL,
    unknown_ms INTEGER NOT NULL,
    peak INTEGER NOT NULL,
    PRIMARY KEY(account_id, bucket)
  );
`);
