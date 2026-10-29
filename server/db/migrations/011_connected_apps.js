'use strict';

// Keys an app received through "Connect with NeoRouter" remember the app:
// its name, where the key was sent and what it said it would use.
exports.up = (db) => {
  db.exec(`ALTER TABLE router_keys ADD COLUMN app_name TEXT;
    ALTER TABLE router_keys ADD COLUMN app_origin TEXT;
    ALTER TABLE router_keys ADD COLUMN app_scopes_json TEXT;`);
};
