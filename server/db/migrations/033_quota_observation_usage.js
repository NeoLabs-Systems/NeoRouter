'use strict';

// Local consumption already represented by an upstream counter. Later sends
// are subtracted from its remaining balance, including within one minute.
exports.up = (db) => db.exec('ALTER TABLE quota_state ADD COLUMN local_used REAL; ALTER TABLE quota_state ADD COLUMN request_cost REAL');
