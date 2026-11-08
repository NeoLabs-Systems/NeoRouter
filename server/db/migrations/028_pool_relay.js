'use strict';

// Members without a public address take part through the pool's host: they
// keep a connection open to it and the host passes traffic along, sealed end
// to end between the two members (services/pools/relay).
//   pool_identities  an X25519 key pair per person, for sealing
//   pool_members     each member's sealing key (signed with its pool key) and
//                    what its NeoRouter supports
//   pools            relay_enabled: the host passes traffic for this pool;
//                    host_relay: our host offers that (members' side)
exports.up = (db) => {
  db.exec(`
    ALTER TABLE pool_identities ADD COLUMN box_public_key TEXT;
    ALTER TABLE pool_identities ADD COLUMN box_private_key_encrypted TEXT;
    ALTER TABLE pool_members ADD COLUMN box_key TEXT;
    ALTER TABLE pool_members ADD COLUMN box_sig TEXT;
    ALTER TABLE pool_members ADD COLUMN features_json TEXT NOT NULL DEFAULT '[]';
    ALTER TABLE pools ADD COLUMN relay_enabled INTEGER NOT NULL DEFAULT 1;
    ALTER TABLE pools ADD COLUMN host_relay INTEGER NOT NULL DEFAULT 0;
  `);
};

exports.down = (db) => {
  db.exec(`
    ALTER TABLE pool_identities DROP COLUMN box_public_key;
    ALTER TABLE pool_identities DROP COLUMN box_private_key_encrypted;
    ALTER TABLE pool_members DROP COLUMN box_key;
    ALTER TABLE pool_members DROP COLUMN box_sig;
    ALTER TABLE pool_members DROP COLUMN features_json;
    ALTER TABLE pools DROP COLUMN relay_enabled;
    ALTER TABLE pools DROP COLUMN host_relay;
  `);
};
