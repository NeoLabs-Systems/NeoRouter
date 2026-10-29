'use strict';

// Teams: an owner invites other accounts through links and decides per member
// what they may do in the owner's space. Things a member creates still belong
// to the owner; created_by_user_id only records who made them.
exports.up = (db) => {
  db.exec(`
    CREATE TABLE team_invites (
      id TEXT PRIMARY KEY,
      owner_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      token_hash TEXT NOT NULL UNIQUE,
      label TEXT NOT NULL DEFAULT '',
      permissions_json TEXT NOT NULL DEFAULT '[]',
      max_uses INTEGER CHECK (max_uses IS NULL OR max_uses > 0),
      use_count INTEGER NOT NULL DEFAULT 0,
      expires_at TEXT,
      revoked_at TEXT,
      created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
    );
    CREATE INDEX idx_team_invites_owner ON team_invites(owner_user_id, created_at);

    CREATE TABLE team_members (
      member_user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
      owner_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      permissions_json TEXT NOT NULL DEFAULT '[]',
      invite_id TEXT REFERENCES team_invites(id) ON DELETE SET NULL,
      created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
      CHECK (member_user_id <> owner_user_id)
    );
    CREATE INDEX idx_team_members_owner ON team_members(owner_user_id);

    CREATE TABLE team_audit (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      action TEXT NOT NULL,
      actor_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
      subject_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
      detail_json TEXT NOT NULL DEFAULT '{}',
      created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
    );
    CREATE INDEX idx_team_audit_time ON team_audit(created_at);

    ALTER TABLE router_keys ADD COLUMN created_by_user_id TEXT REFERENCES users(id) ON DELETE SET NULL;
    ALTER TABLE provider_accounts ADD COLUMN created_by_user_id TEXT REFERENCES users(id) ON DELETE SET NULL;
    ALTER TABLE routing_profiles ADD COLUMN created_by_user_id TEXT REFERENCES users(id) ON DELETE SET NULL;

    UPDATE router_keys SET created_by_user_id = user_id;
    UPDATE provider_accounts SET created_by_user_id = user_id;
    UPDATE routing_profiles SET created_by_user_id = user_id;
  `);
};
