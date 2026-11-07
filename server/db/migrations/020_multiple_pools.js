'use strict';

// A person may be in several pools. Everything that belonged to "the" pool of
// a person now belongs to one of their pools; partner links stay one per pool
// and partner, so each pool keeps its own sharing, caps and pause switch.
// Which connections are shared moves from a flag on the connection to a list
// per pool. Existing pools, members, invites, links and shares are kept.
exports.up = (db) => {
  db.exec(`
    CREATE TABLE pools_new (
      user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      pool_id TEXT NOT NULL,
      name TEXT NOT NULL,
      is_host INTEGER NOT NULL DEFAULT 0,
      host_member_id TEXT NOT NULL,
      host_public_key TEXT NOT NULL,
      host_url TEXT NOT NULL,
      self_url TEXT NOT NULL,
      self_name TEXT NOT NULL,
      share_json TEXT NOT NULL DEFAULT '{}',
      paused INTEGER NOT NULL DEFAULT 0,
      joined_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
      last_sync_at TEXT,
      PRIMARY KEY (user_id, pool_id)
    );
    INSERT INTO pools_new SELECT user_id, pool_id, name, is_host, host_member_id, host_public_key, host_url, self_url, self_name, share_json, paused, joined_at, last_sync_at FROM pools;

    CREATE TABLE pool_members_new (
      user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      pool_id TEXT NOT NULL,
      member_id TEXT NOT NULL,
      name TEXT NOT NULL,
      url TEXT NOT NULL,
      public_key TEXT NOT NULL,
      joined_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
      PRIMARY KEY (user_id, pool_id, member_id)
    );
    INSERT INTO pool_members_new SELECT m.user_id, p.pool_id, m.member_id, m.name, m.url, m.public_key, m.joined_at
      FROM pool_members m JOIN pools p ON p.user_id = m.user_id;

    CREATE TABLE pool_invites_new (
      id TEXT PRIMARY KEY,
      user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      pool_id TEXT NOT NULL,
      token_hash TEXT NOT NULL UNIQUE,
      expires_at INTEGER NOT NULL,
      used_at TEXT,
      used_by TEXT,
      revoked_at TEXT,
      created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
    );
    INSERT INTO pool_invites_new SELECT i.id, i.user_id, p.pool_id, i.token_hash, i.expires_at, i.used_at, i.used_by, i.revoked_at, i.created_at
      FROM pool_invites i JOIN pools p ON p.user_id = i.user_id;

    CREATE TABLE peer_links_new (
      id TEXT PRIMARY KEY,
      user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      pool_id TEXT NOT NULL,
      member_id TEXT NOT NULL,
      name TEXT NOT NULL,
      url TEXT NOT NULL,
      public_key TEXT NOT NULL,
      account_id TEXT REFERENCES provider_accounts(id) ON DELETE SET NULL,
      router_key_id TEXT REFERENCES router_keys(id) ON DELETE SET NULL,
      status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused')),
      last_error TEXT,
      created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
      UNIQUE (user_id, pool_id, member_id)
    );
    INSERT INTO peer_links_new SELECT l.id, l.user_id, p.pool_id, l.member_id, l.name, l.url, l.public_key, l.account_id, l.router_key_id, l.status, l.last_error, l.created_at
      FROM peer_links l JOIN pools p ON p.user_id = l.user_id;

    CREATE TABLE pool_shared_accounts (
      user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      pool_id TEXT NOT NULL,
      account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
      PRIMARY KEY (user_id, pool_id, account_id)
    );
    INSERT INTO pool_shared_accounts SELECT a.user_id, p.pool_id, a.id FROM provider_accounts a JOIN pools p ON p.user_id = a.user_id WHERE a.pool_share = 1;
  `);
  // peer_usage_daily points at peer_links; keep its rows while the table is swapped.
  db.exec(`
    CREATE TABLE peer_usage_keep AS SELECT * FROM peer_usage_daily;
    DROP TABLE peer_usage_daily;
    DROP TABLE peer_links;
    ALTER TABLE peer_links_new RENAME TO peer_links;
    CREATE TABLE peer_usage_daily (
      link_id TEXT NOT NULL REFERENCES peer_links(id) ON DELETE CASCADE,
      day TEXT NOT NULL,
      direction TEXT NOT NULL CHECK (direction IN ('given', 'taken')),
      requests INTEGER NOT NULL DEFAULT 0,
      tokens INTEGER NOT NULL DEFAULT 0,
      PRIMARY KEY (link_id, day, direction)
    );
    INSERT INTO peer_usage_daily SELECT * FROM peer_usage_keep WHERE link_id IN (SELECT id FROM peer_links);
    DROP TABLE peer_usage_keep;
    DROP TABLE pools;
    ALTER TABLE pools_new RENAME TO pools;
    DROP TABLE pool_members;
    ALTER TABLE pool_members_new RENAME TO pool_members;
    DROP TABLE pool_invites;
    ALTER TABLE pool_invites_new RENAME TO pool_invites;
    CREATE INDEX idx_pool_invites_user ON pool_invites(user_id, pool_id);
    CREATE INDEX idx_peer_links_account ON peer_links(account_id);
  `);
};

exports.down = () => {
  throw new Error('Going back to one pool per person is not supported.');
};
