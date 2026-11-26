-- A database written by the Node.js server (its 41 migrations, then one row of
-- each thing an upgrade has to keep). Values are fixtures, sealed with the key
-- the migration test writes.
PRAGMA foreign_keys=OFF;
BEGIN TRANSACTION;
CREATE TABLE schema_migrations (
    version INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
  );
INSERT INTO schema_migrations VALUES(1,'001_foundation.js','2026-11-26T10:45:27.967Z');
INSERT INTO schema_migrations VALUES(2,'002_min_output_tokens.js','2026-11-26T10:45:27.970Z');
INSERT INTO schema_migrations VALUES(3,'003_two_factor.js','2026-11-26T10:45:27.973Z');
INSERT INTO schema_migrations VALUES(4,'004_billing_state.js','2026-11-26T10:45:27.974Z');
INSERT INTO schema_migrations VALUES(5,'005_saved_usd.js','2026-11-26T10:45:27.975Z');
INSERT INTO schema_migrations VALUES(6,'006_processing_stats.js','2026-11-26T10:45:27.976Z');
INSERT INTO schema_migrations VALUES(7,'007_teams.js','2026-11-26T10:45:27.977Z');
INSERT INTO schema_migrations VALUES(8,'008_capabilities.js','2026-11-26T10:45:27.978Z');
INSERT INTO schema_migrations VALUES(9,'009_prepaid_plans.js','2026-11-26T10:45:27.978Z');
INSERT INTO schema_migrations VALUES(10,'010_key_model_kinds.js','2026-11-26T10:45:27.979Z');
INSERT INTO schema_migrations VALUES(11,'011_connected_apps.js','2026-11-26T10:45:27.980Z');
INSERT INTO schema_migrations VALUES(12,'012_three_presets.js','2026-11-26T10:45:27.980Z');
INSERT INTO schema_migrations VALUES(13,'013_key_token_limits.js','2026-11-26T10:45:27.981Z');
INSERT INTO schema_migrations VALUES(14,'014_task_routing.js','2026-11-26T10:45:27.982Z');
INSERT INTO schema_migrations VALUES(15,'015_queued_ms.js','2026-11-26T10:45:27.983Z');
INSERT INTO schema_migrations VALUES(16,'016_key_priority.js','2026-11-26T10:45:27.984Z');
INSERT INTO schema_migrations VALUES(17,'017_max_price_per_million.js','2026-11-26T10:45:27.984Z');
INSERT INTO schema_migrations VALUES(18,'018_account_limits.js','2026-11-26T10:45:27.985Z');
INSERT INTO schema_migrations VALUES(19,'019_pools.js','2026-11-26T10:45:27.989Z');
INSERT INTO schema_migrations VALUES(20,'020_multiple_pools.js','2026-11-26T10:45:28.006Z');
INSERT INTO schema_migrations VALUES(21,'021_usage_summary_index.js','2026-11-26T10:45:28.007Z');
INSERT INTO schema_migrations VALUES(22,'022_pool_paid_sharing.js','2026-11-26T10:45:28.008Z');
INSERT INTO schema_migrations VALUES(23,'023_pool_share_level.js','2026-11-26T10:45:28.010Z');
INSERT INTO schema_migrations VALUES(24,'024_pool_share_kinds.js','2026-11-26T10:45:28.011Z');
INSERT INTO schema_migrations VALUES(25,'025_account_classification.js','2026-11-26T10:45:28.012Z');
INSERT INTO schema_migrations VALUES(26,'026_clear_media_cooldowns.js','2026-11-26T10:45:28.013Z');
INSERT INTO schema_migrations VALUES(27,'027_local_models.js','2026-11-26T10:45:28.013Z');
INSERT INTO schema_migrations VALUES(28,'028_pool_relay.js','2026-11-26T10:45:28.014Z');
INSERT INTO schema_migrations VALUES(29,'029_confirm_failures.js','2026-11-26T10:45:28.015Z');
INSERT INTO schema_migrations VALUES(30,'030_output_floor_time.js','2026-11-26T10:45:28.015Z');
INSERT INTO schema_migrations VALUES(31,'031_routing_groups_cache.js','2026-11-26T10:45:28.016Z');
INSERT INTO schema_migrations VALUES(32,'032_flat_rate_slots.js','2026-11-26T10:45:28.017Z');
INSERT INTO schema_migrations VALUES(33,'033_quota_observation_usage.js','2026-11-26T10:45:28.017Z');
INSERT INTO schema_migrations VALUES(34,'034_forgive_budget_truncations.js','2026-11-26T10:45:28.018Z');
INSERT INTO schema_migrations VALUES(35,'035_outbound_proxies.js','2026-11-26T10:45:28.018Z');
INSERT INTO schema_migrations VALUES(36,'036_discovery_news.js','2026-11-26T10:45:28.018Z');
INSERT INTO schema_migrations VALUES(37,'037_local_benchmarks.js','2026-11-26T10:45:28.018Z');
INSERT INTO schema_migrations VALUES(38,'038_remove_local_benchmarks.js','2026-11-26T10:45:28.019Z');
INSERT INTO schema_migrations VALUES(39,'039_router_key_concurrency.js','2026-11-26T10:45:28.019Z');
INSERT INTO schema_migrations VALUES(40,'040_benchmark_evidence.js','2026-11-26T10:45:28.020Z');
INSERT INTO schema_migrations VALUES(41,'041_remove_replaybench.js','2026-11-26T10:45:28.022Z');
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
INSERT INTO users VALUES('u-fixture','fixture','fixture@example.test',NULL,'$2b$04$FQo88PGWxet/JNgF64.dzucFEK.a5Cm9MKn4knxOcnVDMMoz.Nhty','admin',NULL,'2026-11-26T10:45:28.023Z','2026-11-26T10:45:28.023Z',NULL);
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
INSERT INTO user_sessions VALUES('s-fixture','u-fixture','0d7121b75e053a98cf5c74197ca400882fd978330ec323c8ed8893d632402e39','2099-01-01T00:00:00.000Z',NULL,NULL,'2026-11-26T10:45:28.024Z',NULL,NULL);
CREATE TABLE settings (
      key TEXT PRIMARY KEY,
      value TEXT NOT NULL
    );
INSERT INTO settings VALUES('artificial_analysis_key','"zv1glc9qTwAlFPecFxFYWLwVPLhiPV6YTLTMNIGt0fViNd22Lr6imdEbHxFAjqw5"');
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
INSERT INTO provider_accounts VALUES('a-fixture','u-fixture','openai','openai','OpenAI',NULL,'uvBSLpJBtZktcS8p7w1-9hYTiWA57LXACRaMKR3Abwyn3SeWDELri644unbo0a5H','{}','paid',1,'unknown',NULL,0,NULL,NULL,'2026-11-26T10:45:28.024Z','unknown',NULL,NULL,NULL,0,0,NULL,'auto',NULL);
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
CREATE TABLE budget_ledger (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      router_key_id TEXT NOT NULL REFERENCES router_keys(id) ON DELETE CASCADE,
      request_id TEXT,
      amount_usd REAL NOT NULL,
      created_at INTEGER NOT NULL
    );
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
INSERT INTO user_two_factor VALUES('u-fixture','eGXXa-7PHAIUHVBSOgxmfyLmV9Urllatoejj7hJ7yTAtgdSIBh5jMvOVxe0',NULL,1,NULL,0,0,'2026-11-26T10:45:28.024Z');
CREATE TABLE user_recovery_codes (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      code_hash TEXT NOT NULL,
      used_at TEXT
    );
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
CREATE TABLE team_members (
      member_user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
      owner_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      permissions_json TEXT NOT NULL DEFAULT '[]',
      invite_id TEXT REFERENCES team_invites(id) ON DELETE SET NULL,
      created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
      CHECK (member_user_id <> owner_user_id)
    );
CREATE TABLE team_audit (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      action TEXT NOT NULL,
      actor_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
      subject_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
      detail_json TEXT NOT NULL DEFAULT '{}',
      created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
    );
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
CREATE TABLE IF NOT EXISTS "pools" (
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
CREATE TABLE IF NOT EXISTS "pool_members" (
      user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      pool_id TEXT NOT NULL,
      member_id TEXT NOT NULL,
      name TEXT NOT NULL,
      url TEXT NOT NULL,
      public_key TEXT NOT NULL,
      joined_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')), box_key TEXT, box_sig TEXT, features_json TEXT NOT NULL DEFAULT '[]',
      PRIMARY KEY (user_id, pool_id, member_id)
    );
CREATE TABLE IF NOT EXISTS "pool_invites" (
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
CREATE TABLE IF NOT EXISTS "peer_links" (
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
CREATE INDEX idx_user_sessions_user ON user_sessions(user_id);
CREATE INDEX idx_models_canonical ON models(canonical_id);
CREATE INDEX idx_router_keys_user ON router_keys(user_id);
CREATE INDEX idx_requests_user_time ON requests(user_id, created_at);
CREATE INDEX idx_requests_key_time ON requests(router_key_id, created_at);
CREATE INDEX idx_budget_ledger_key_time ON budget_ledger(router_key_id, created_at);
CREATE INDEX idx_user_recovery_codes_user ON user_recovery_codes(user_id);
CREATE INDEX idx_team_invites_owner ON team_invites(owner_user_id, created_at);
CREATE INDEX idx_team_members_owner ON team_members(owner_user_id);
CREATE INDEX idx_team_audit_time ON team_audit(created_at);
CREATE INDEX requests_user_capability ON requests (user_id, capability, created_at);
CREATE INDEX media_jobs_user ON media_jobs (user_id, created_at);
CREATE INDEX idx_pool_invites_user ON pool_invites(user_id, pool_id);
CREATE INDEX idx_peer_links_account ON peer_links(account_id);
CREATE INDEX requests_user_time_summary ON requests
    (user_id, created_at, status, is_free, attempts, cost_usd, saved_usd, input_tokens, output_tokens, latency_ms, ttft_ms, router_key_id, model_label, error_code);
CREATE INDEX idx_confirm_failures_user ON confirm_failures(user_id, failed_at_ms);
CREATE INDEX discovery_news_timeline ON discovery_news(detected_at DESC, id);
COMMIT;
