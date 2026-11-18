'use strict';

// Wrong passwords and codes typed by a signed-in account when it is asked
// who it is again (changing the password, exporting or deleting the account,
// 2FA settings), counted per account: services/auth/confirm_throttle.
exports.up = (db) => {
  db.exec(`
    CREATE TABLE confirm_failures (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      kind TEXT NOT NULL,
      failed_at_ms INTEGER NOT NULL
    );
    CREATE INDEX idx_confirm_failures_user ON confirm_failures(user_id, failed_at_ms);
  `);
};

exports.down = (db) => {
  db.exec('DROP TABLE confirm_failures;');
};
