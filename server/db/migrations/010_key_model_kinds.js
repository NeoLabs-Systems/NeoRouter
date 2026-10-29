'use strict';

// Which kinds of models a key may use, as three independent switches: free,
// prepaid plans and paid (the latter is `mode` != 'free_only', as before).
exports.up = (db) => {
  db.exec(`ALTER TABLE router_keys ADD COLUMN allow_free INTEGER NOT NULL DEFAULT 1;
    ALTER TABLE router_keys ADD COLUMN allow_plan INTEGER NOT NULL DEFAULT 1;`);
};
