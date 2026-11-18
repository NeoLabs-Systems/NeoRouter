'use strict';

// The person's own call on a whole connection: every model free, or every
// model paid, whatever NeoRouter worked out. NULL keeps the automatic result.
exports.up = (db) => {
  db.exec("ALTER TABLE provider_accounts ADD COLUMN classification TEXT CHECK (classification IN ('free', 'paid'))");
};

exports.down = (db) => {
  db.exec('ALTER TABLE provider_accounts DROP COLUMN classification');
};
