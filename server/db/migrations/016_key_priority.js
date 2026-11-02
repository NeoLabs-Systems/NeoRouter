'use strict';

// Who goes first when models are busy: 'high', 'normal' or 'low'.
exports.up = (db) => {
  db.exec("ALTER TABLE router_keys ADD COLUMN priority TEXT NOT NULL DEFAULT 'normal';");
};
