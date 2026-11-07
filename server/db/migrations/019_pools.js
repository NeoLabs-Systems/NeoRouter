'use strict';

// Pools: separate NeoRouter instances (one person's space on each) that share
// their free capacity. No provider key ever leaves an instance: a partner's
// request runs on the partner's own connections and only the answer comes back.
//
//   pool_identities  one signing key pair per local person taking part
//   pools            the pool this person is in (at most one), and its host
//   pool_members     who is in the pool, as this instance knows it
//   pool_invites     one-time join links (hashed), kept by the host
//   peer_links       one partner: the account we use them through (outbound)
//                    and the key they use us with (inbound)
//   peer_usage_daily what was given to and taken from each partner, per UTC day
exports.up = (db) => {
  db.exec(`
    CREATE TABLE pool_identities (
      user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
      member_id TEXT NOT NULL UNIQUE,
      public_key TEXT NOT NULL,
      private_key_encrypted TEXT NOT NULL,
      created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
    );

    CREATE TABLE pools (
      user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
      pool_id TEXT NOT NULL,
      name TEXT NOT NULL,
      is_host INTEGER NOT NULL DEFAULT 0,
      host_member_id TEXT NOT NULL,
      host_public_key TEXT NOT NULL,
      host_url TEXT NOT NULL,
      self_url TEXT NOT NULL,
      self_name TEXT NOT NULL,
      share_json TEXT NOT NULL DEFAULT '{}',
      -- Switched off for now: nothing is given to or taken from partners.
      paused INTEGER NOT NULL DEFAULT 0,
      joined_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
      last_sync_at TEXT
    );

    CREATE TABLE pool_members (
      user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      member_id TEXT NOT NULL,
      name TEXT NOT NULL,
      url TEXT NOT NULL,
      public_key TEXT NOT NULL,
      joined_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
      PRIMARY KEY (user_id, member_id)
    );

    CREATE TABLE pool_invites (
      id TEXT PRIMARY KEY,
      user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      token_hash TEXT NOT NULL UNIQUE,
      expires_at INTEGER NOT NULL,
      used_at TEXT,
      used_by TEXT,
      revoked_at TEXT,
      created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
    );
    CREATE INDEX idx_pool_invites_user ON pool_invites(user_id);

    CREATE TABLE peer_links (
      id TEXT PRIMARY KEY,
      user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      member_id TEXT NOT NULL,
      name TEXT NOT NULL,
      url TEXT NOT NULL,
      public_key TEXT NOT NULL,
      account_id TEXT REFERENCES provider_accounts(id) ON DELETE SET NULL,
      router_key_id TEXT REFERENCES router_keys(id) ON DELETE SET NULL,
      status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused')),
      last_error TEXT,
      created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
      UNIQUE (user_id, member_id)
    );

    CREATE TABLE peer_usage_daily (
      link_id TEXT NOT NULL REFERENCES peer_links(id) ON DELETE CASCADE,
      day TEXT NOT NULL,
      direction TEXT NOT NULL CHECK (direction IN ('given', 'taken')),
      requests INTEGER NOT NULL DEFAULT 0,
      tokens INTEGER NOT NULL DEFAULT 0,
      PRIMARY KEY (link_id, day, direction)
    );

    ALTER TABLE router_keys ADD COLUMN pool_access TEXT NOT NULL DEFAULT 'own';
    ALTER TABLE router_keys ADD COLUMN peer_link_id TEXT;
    ALTER TABLE provider_accounts ADD COLUMN pool_share INTEGER NOT NULL DEFAULT 0;
    ALTER TABLE requests ADD COLUMN kept_private TEXT;
  `);
};

exports.down = (db) => {
  db.exec(`
    DROP TABLE peer_usage_daily;
    DROP TABLE peer_links;
    DROP TABLE pool_invites;
    DROP TABLE pool_members;
    DROP TABLE pools;
    DROP TABLE pool_identities;
  `);
};
