'use strict';

// Which kinds of a connection's models a pool gets are chosen one by one
// (free, plan = subscription, paid) instead of as one level from 023. A level
// included the ones before it, so "plan" becomes free + plan and "paid"
// becomes free + plan + paid: everyone keeps sharing exactly what they did.
exports.up = (db) => {
  db.exec(`
    ALTER TABLE pool_shared_accounts ADD COLUMN share_free INTEGER NOT NULL DEFAULT 1;
    ALTER TABLE pool_shared_accounts ADD COLUMN share_plan INTEGER NOT NULL DEFAULT 0;
    ALTER TABLE pool_shared_accounts ADD COLUMN share_paid INTEGER NOT NULL DEFAULT 0;
    UPDATE pool_shared_accounts SET share_plan = 1 WHERE share_level IN ('plan', 'paid');
    UPDATE pool_shared_accounts SET share_paid = 1 WHERE share_level = 'paid';
    ALTER TABLE pool_shared_accounts DROP COLUMN share_level;
  `);
};

exports.down = () => {
  throw new Error('Going back to one sharing level per connection is not supported.');
};
