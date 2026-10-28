'use strict';

// Non-chat kinds (embeddings, images, audio, video, System One) share the
// request log and the catalog. They are billed per unit (an image, a second of
// video, a minute of audio, a character of speech) instead of per token.
exports.up = (db) => {
  db.exec(`ALTER TABLE requests ADD COLUMN capability TEXT NOT NULL DEFAULT 'chat';
    ALTER TABLE requests ADD COLUMN units REAL NOT NULL DEFAULT 0;
    ALTER TABLE models ADD COLUMN price_unit TEXT;
    ALTER TABLE models ADD COLUMN price_per_unit REAL;
    CREATE INDEX IF NOT EXISTS requests_user_capability ON requests (user_id, capability, created_at);
    CREATE TABLE IF NOT EXISTS media_jobs (
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
    CREATE INDEX IF NOT EXISTS media_jobs_user ON media_jobs (user_id, created_at);
    -- Vectors from different embedding models (or dimensions) cannot be
    -- compared, so "auto" embeddings stick to the first model a key used.
    CREATE TABLE IF NOT EXISTS embedding_pins (
      router_key_id TEXT PRIMARY KEY REFERENCES router_keys(id) ON DELETE CASCADE,
      canonical_id TEXT NOT NULL,
      dimensions INTEGER,
      created_at INTEGER NOT NULL
    );
    -- Audio is two kinds with opposite directions.
    UPDATE models SET capability = CASE
      WHEN model_id GLOB '*[Ww]hisper*' OR lower(model_id) LIKE '%transcri%' OR lower(model_id) LIKE '%asr%' THEN 'transcription'
      ELSE 'speech' END
    WHERE capability = 'audio';`);
};
