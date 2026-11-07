'use strict';

// How much of a connection a pool gets is one level per connection and pool:
// free (only its free models), plan (also its subscription models) or paid
// (also pay-as-you-go models). It replaces the paid flag from 022.
exports.up = (db) => {
  db.exec(`
    ALTER TABLE pool_shared_accounts ADD COLUMN share_level TEXT NOT NULL DEFAULT 'free' CHECK (share_level IN ('free', 'plan', 'paid'));
    UPDATE pool_shared_accounts SET share_level = 'paid' WHERE share_paid = 1;
    ALTER TABLE pool_shared_accounts DROP COLUMN share_paid;
  `);
};

exports.down = () => {
  throw new Error('Going back to the paid flag is not supported.');
};
