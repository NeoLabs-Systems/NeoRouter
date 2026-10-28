'use strict';

exports.up = (db) => {
  db.exec(`
    CREATE TABLE users (
      id TEXT PRIMARY KEY,
      username TEXT NOT NULL UNIQUE COLLATE NOCASE,
      email TEXT NOT NULL UNIQUE COLLATE NOCASE,
      display_name TEXT,
      password_hash TEXT NOT NULL,
      role TEXT NOT NULL CHECK (role IN ('admin', 'user')),
      disabled_at TEXT,
      created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
      updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
      last_login_at TEXT
    );

    CREATE TABLE user_sessions (
      id TEXT PRIMARY KEY,
      user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      token_hash TEXT NOT NULL UNIQUE,
      expires_at TEXT NOT NULL,
      ip_address TEXT,
      user_agent TEXT,
      created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
      last_used_at TEXT,
      revoked_at TEXT
    );
    CREATE INDEX idx_user_sessions_user ON user_sessions(user_id);

    CREATE TABLE settings (
      key TEXT PRIMARY KEY,
      value TEXT NOT NULL
    );

    CREATE TABLE user_settings (
      user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      key TEXT NOT NULL,
      value TEXT NOT NULL,
      PRIMARY KEY (user_id, key)
    );

    CREATE TABLE provider_accounts (
      id TEXT PRIMARY KEY,
      user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      definition_id TEXT NOT NULL,
      slug TEXT NOT NULL,
      label TEXT NOT NULL,
      base_url TEXT,
      secret_encrypted TEXT,
      options_json TEXT NOT NULL DEFAULT '{}',
      billing_mode TEXT NOT NULL CHECK (billing_mode IN ('free_tier', 'paid')),
      enabled INTEGER NOT NULL DEFAULT 1,
      status TEXT NOT NULL DEFAULT 'unknown' CHECK (status IN ('unknown', 'healthy', 'invalid', 'error', 'disabled')),
      status_message TEXT,
      auth_failures INTEGER NOT NULL DEFAULT 0,
      last_validated_at TEXT,
      last_synced_at TEXT,
      created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
      UNIQUE (user_id, slug)
    );

    CREATE TABLE models (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
      model_id TEXT NOT NULL,
      display_name TEXT,
      canonical_id TEXT NOT NULL,
      capability TEXT NOT NULL DEFAULT 'chat',
      context_window INTEGER,
      max_output INTEGER,
      supports_tools INTEGER,
      supports_vision INTEGER,
      supports_json INTEGER,
      supports_reasoning INTEGER,
      price_in REAL,
      price_out REAL,
      price_source TEXT,
      is_free INTEGER NOT NULL DEFAULT 0,
      free_reason TEXT,
      enabled INTEGER NOT NULL DEFAULT 1,
      available INTEGER NOT NULL DEFAULT 1,
      overrides_json TEXT NOT NULL DEFAULT '{}',
      raw_json TEXT NOT NULL DEFAULT '{}',
      first_seen_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
      last_seen_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
      UNIQUE (account_id, model_id)
    );
    CREATE INDEX idx_models_canonical ON models(canonical_id);

    CREATE TABLE model_metadata (
      canonical_id TEXT PRIMARY KEY,
      intelligence REAL,
      intelligence_source TEXT,
      intelligence_confidence REAL,
      speed_tps REAL,
      context_window INTEGER,
      max_output INTEGER,
      price_in REAL,
      price_out REAL,
      supports_tools INTEGER,
      supports_vision INTEGER,
      supports_reasoning INTEGER,
      release_date TEXT,
      params_b REAL,
      sources_json TEXT NOT NULL DEFAULT '[]',
      updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
    );

    CREATE TABLE model_stats (
      model_ref INTEGER PRIMARY KEY REFERENCES models(id) ON DELETE CASCADE,
      successes REAL NOT NULL DEFAULT 0,
      failures REAL NOT NULL DEFAULT 0,
      decayed_at INTEGER NOT NULL DEFAULT 0,
      latency_ms REAL,
      ttft_ms REAL,
      tps REAL,
      samples INTEGER NOT NULL DEFAULT 0,
      penalty REAL NOT NULL DEFAULT 0,
      penalty_at INTEGER NOT NULL DEFAULT 0,
      cooldown_until INTEGER NOT NULL DEFAULT 0,
      cooldown_reason TEXT,
      cooldown_level INTEGER NOT NULL DEFAULT 0,
      recent_failures_json TEXT NOT NULL DEFAULT '[]',
      breaker_open_until INTEGER NOT NULL DEFAULT 0,
      last_error TEXT,
      last_success_at INTEGER,
      last_failure_at INTEGER
    );

    CREATE TABLE quota_buckets (
      scope_key TEXT NOT NULL,
      minute INTEGER NOT NULL,
      requests INTEGER NOT NULL DEFAULT 0,
      tokens INTEGER NOT NULL DEFAULT 0,
      PRIMARY KEY (scope_key, minute)
    );

    CREATE TABLE quota_limits (
      scope_key TEXT NOT NULL,
      metric TEXT NOT NULL CHECK (metric IN ('rpm', 'rpd', 'tpm', 'tpd')),
      value INTEGER NOT NULL,
      source TEXT NOT NULL,
      updated_at INTEGER NOT NULL,
      PRIMARY KEY (scope_key, metric)
    );

    CREATE TABLE quota_state (
      scope_key TEXT NOT NULL,
      metric TEXT NOT NULL,
      limit_value REAL,
      remaining REAL,
      reset_at INTEGER,
      source TEXT NOT NULL,
      observed_at INTEGER NOT NULL,
      PRIMARY KEY (scope_key, metric)
    );

    CREATE TABLE routing_profiles (
      id TEXT PRIMARY KEY,
      user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      name TEXT NOT NULL,
      weights_json TEXT NOT NULL,
      constraints_json TEXT NOT NULL DEFAULT '{}',
      created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
      updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
    );

    CREATE TABLE router_keys (
      id TEXT PRIMARY KEY,
      user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      name TEXT NOT NULL,
      prefix TEXT NOT NULL UNIQUE,
      key_hash TEXT NOT NULL UNIQUE,
      mode TEXT NOT NULL CHECK (mode IN ('free_only', 'paid', 'both')),
      budget_usd REAL,
      budget_period TEXT NOT NULL DEFAULT 'total' CHECK (budget_period IN ('total', 'monthly', 'daily')),
      rpm_limit INTEGER,
      allowed_models_json TEXT NOT NULL DEFAULT '[]',
      profile TEXT NOT NULL DEFAULT 'balanced',
      expires_at TEXT,
      revoked_at TEXT,
      last_used_at TEXT,
      created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
    );
    CREATE INDEX idx_router_keys_user ON router_keys(user_id);

    CREATE TABLE requests (
      id TEXT PRIMARY KEY,
      user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      router_key_id TEXT REFERENCES router_keys(id) ON DELETE SET NULL,
      created_at INTEGER NOT NULL,
      api_format TEXT NOT NULL,
      requested_model TEXT NOT NULL,
      model_ref INTEGER REFERENCES models(id) ON DELETE SET NULL,
      model_label TEXT,
      account_id TEXT,
      status TEXT NOT NULL CHECK (status IN ('success', 'error')),
      http_status INTEGER,
      error_code TEXT,
      error_message TEXT,
      input_tokens INTEGER NOT NULL DEFAULT 0,
      output_tokens INTEGER NOT NULL DEFAULT 0,
      cost_usd REAL NOT NULL DEFAULT 0,
      is_free INTEGER NOT NULL DEFAULT 0,
      latency_ms INTEGER,
      ttft_ms INTEGER,
      attempts INTEGER NOT NULL DEFAULT 1,
      stream INTEGER NOT NULL DEFAULT 0,
      trail_json TEXT NOT NULL DEFAULT '[]'
    );
    CREATE INDEX idx_requests_user_time ON requests(user_id, created_at);
    CREATE INDEX idx_requests_key_time ON requests(router_key_id, created_at);

    CREATE TABLE budget_ledger (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      router_key_id TEXT NOT NULL REFERENCES router_keys(id) ON DELETE CASCADE,
      request_id TEXT,
      amount_usd REAL NOT NULL,
      created_at INTEGER NOT NULL
    );
    CREATE INDEX idx_budget_ledger_key_time ON budget_ledger(router_key_id, created_at);
  `);
};
