-- NeoRouter baseline schema: produced by Node migrations 001..041.
-- Generated from a scratch better-sqlite3 database; do not edit by hand.

CREATE TABLE schema_migrations (
    version INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
  );

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
      created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')), billing_state TEXT NOT NULL DEFAULT 'unknown', billing_message TEXT, billing_checked_at TEXT, created_by_user_id TEXT REFERENCES users(id) ON DELETE SET NULL, prepaid INTEGER NOT NULL DEFAULT 0, pool_share INTEGER NOT NULL DEFAULT 0, classification TEXT CHECK (classification IN ('free', 'paid')), overflow_policy TEXT NOT NULL DEFAULT 'auto' CHECK(overflow_policy IN ('auto', 'overflow', 'queue')), slot_wait_ms INTEGER CHECK(slot_wait_ms IS NULL OR slot_wait_ms >= 0),
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
      last_seen_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')), price_unit TEXT, price_per_unit REAL,
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
    , skills_json TEXT);

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
    , min_output_tokens INTEGER NOT NULL DEFAULT 0, min_output_at INTEGER NOT NULL DEFAULT 0);

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
      observed_at INTEGER NOT NULL, local_used REAL, request_cost REAL,
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
    , created_by_user_id TEXT REFERENCES users(id) ON DELETE SET NULL);

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
    , created_by_user_id TEXT REFERENCES users(id) ON DELETE SET NULL, allow_free INTEGER NOT NULL DEFAULT 1, allow_plan INTEGER NOT NULL DEFAULT 1, app_name TEXT, app_origin TEXT, app_scopes_json TEXT, token_limit INTEGER, token_period TEXT NOT NULL DEFAULT 'monthly', priority TEXT NOT NULL DEFAULT 'normal', max_price_per_million REAL, pool_access TEXT NOT NULL DEFAULT 'own', peer_link_id TEXT, routing_group TEXT, concurrent_limit INTEGER CHECK (concurrent_limit > 0));

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
    , saved_usd REAL NOT NULL DEFAULT 0, redacted_count INTEGER NOT NULL DEFAULT 0, compression_saved_tokens INTEGER NOT NULL DEFAULT 0, capability TEXT NOT NULL DEFAULT 'chat', units REAL NOT NULL DEFAULT 0, task TEXT, queued_ms INTEGER NOT NULL DEFAULT 0, kept_private TEXT, routing_group TEXT, cached_input_tokens INTEGER NOT NULL DEFAULT 0, cache_creation_tokens INTEGER NOT NULL DEFAULT 0);

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

CREATE TABLE user_two_factor (
      user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
      secret_encrypted TEXT,
      pending_secret_encrypted TEXT,
      enabled INTEGER NOT NULL DEFAULT 0,
      enabled_at TEXT,
      failed_attempts INTEGER NOT NULL DEFAULT 0,
      locked_until INTEGER NOT NULL DEFAULT 0,
      updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
    );

CREATE TABLE user_recovery_codes (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      code_hash TEXT NOT NULL,
      used_at TEXT
    );

CREATE INDEX idx_user_recovery_codes_user ON user_recovery_codes(user_id);

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

CREATE INDEX requests_user_capability ON requests (user_id, capability, created_at);

CREATE TABLE media_jobs (
      id TEXT PRIMARY KEY,
      user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      router_key_id TEXT REFERENCES router_keys(id) ON DELETE SET NULL,
      request_id TEXT,
      capability TEXT NOT NULL,
      model_ref INTEGER REFERENCES models(id) ON DELETE SET NULL,
      model_label TEXT,
      account_id TEXT,
      upstream_id TEXT NOT NULL,
      status TEXT NOT NULL,
      progress REAL,
      request_json TEXT NOT NULL DEFAULT '{}',
      result_json TEXT,
      error_message TEXT,
      created_at INTEGER NOT NULL,
      updated_at INTEGER NOT NULL
    );

CREATE INDEX media_jobs_user ON media_jobs (user_id, created_at);

CREATE TABLE embedding_pins (
      router_key_id TEXT PRIMARY KEY REFERENCES router_keys(id) ON DELETE CASCADE,
      canonical_id TEXT NOT NULL,
      dimensions INTEGER,
      created_at INTEGER NOT NULL
    );

CREATE TABLE model_task_stats (
      model_ref INTEGER NOT NULL REFERENCES models(id) ON DELETE CASCADE,
      task TEXT NOT NULL,
      successes REAL NOT NULL DEFAULT 0,
      failures REAL NOT NULL DEFAULT 0,
      liked REAL NOT NULL DEFAULT 0,
      disliked REAL NOT NULL DEFAULT 0,
      updated_at INTEGER NOT NULL,
      PRIMARY KEY (model_ref, task)
    );

CREATE TABLE account_limits (
      account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
      metric TEXT NOT NULL CHECK (metric IN ('rpm', 'rpd', 'tpm', 'tpd', 'concurrent')),
      value INTEGER NOT NULL CHECK (value >= 1),
      updated_at INTEGER NOT NULL,
      PRIMARY KEY (account_id, metric)
    );

CREATE TABLE pool_identities (
      user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
      member_id TEXT NOT NULL UNIQUE,
      public_key TEXT NOT NULL,
      private_key_encrypted TEXT NOT NULL,
      created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
    , box_public_key TEXT, box_private_key_encrypted TEXT);

CREATE TABLE "pools" (
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
      last_sync_at TEXT, relay_enabled INTEGER NOT NULL DEFAULT 1, host_relay INTEGER NOT NULL DEFAULT 0,
      PRIMARY KEY (user_id, pool_id)
    );

CREATE TABLE "pool_members" (
      user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      pool_id TEXT NOT NULL,
      member_id TEXT NOT NULL,
      name TEXT NOT NULL,
      url TEXT NOT NULL,
      public_key TEXT NOT NULL,
      joined_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')), box_key TEXT, box_sig TEXT, features_json TEXT NOT NULL DEFAULT '[]',
      PRIMARY KEY (user_id, pool_id, member_id)
    );

CREATE TABLE "pool_invites" (
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

CREATE TABLE "peer_links" (
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

CREATE TABLE pool_shared_accounts (
      user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      pool_id TEXT NOT NULL,
      account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE, share_free INTEGER NOT NULL DEFAULT 1, share_plan INTEGER NOT NULL DEFAULT 0, share_paid INTEGER NOT NULL DEFAULT 0,
      PRIMARY KEY (user_id, pool_id, account_id)
    );

CREATE TABLE peer_usage_daily (
      link_id TEXT NOT NULL REFERENCES peer_links(id) ON DELETE CASCADE,
      day TEXT NOT NULL,
      direction TEXT NOT NULL CHECK (direction IN ('given', 'taken')),
      requests INTEGER NOT NULL DEFAULT 0,
      tokens INTEGER NOT NULL DEFAULT 0,
      PRIMARY KEY (link_id, day, direction)
    );

CREATE INDEX idx_pool_invites_user ON pool_invites(user_id, pool_id);

CREATE INDEX idx_peer_links_account ON peer_links(account_id);

CREATE INDEX requests_user_time_summary ON requests
    (user_id, created_at, status, is_free, attempts, cost_usd, saved_usd, input_tokens, output_tokens, latency_ms, ttft_ms, router_key_id, model_label, error_code);

CREATE TABLE local_models (
      id TEXT PRIMARY KEY,
      status TEXT NOT NULL CHECK (status IN ('downloading', 'installed')),
      enabled INTEGER NOT NULL DEFAULT 1,
      installed_by_user_id TEXT,
      created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
      updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
    );

CREATE TABLE confirm_failures (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      kind TEXT NOT NULL,
      failed_at_ms INTEGER NOT NULL
    );

CREATE INDEX idx_confirm_failures_user ON confirm_failures(user_id, failed_at_ms);

CREATE TABLE routing_groups (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    config_json TEXT NOT NULL,
    UNIQUE(user_id, name)
  );

CREATE TABLE slot_utilization (
    account_id TEXT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
    bucket INTEGER NOT NULL,
    observed_ms INTEGER NOT NULL,
    busy_ms INTEGER NOT NULL,
    capacity_ms INTEGER NOT NULL,
    unknown_ms INTEGER NOT NULL,
    peak INTEGER NOT NULL,
    PRIMARY KEY(account_id, bucket)
  );

CREATE TABLE outbound_proxies (
 id TEXT PRIMARY KEY, fingerprint TEXT UNIQUE NOT NULL, endpoint TEXT NOT NULL,
 secret_encrypted TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE discovery_news (
    id TEXT PRIMARY KEY,
    canonical_id TEXT NOT NULL,
    capability TEXT NOT NULL,
    model_id TEXT NOT NULL,
    name TEXT NOT NULL,
    score REAL NOT NULL,
    source_count INTEGER NOT NULL,
    context_window INTEGER,
    detected_at INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    available INTEGER NOT NULL DEFAULT 1
  );

CREATE INDEX discovery_news_timeline ON discovery_news(detected_at DESC, id);

