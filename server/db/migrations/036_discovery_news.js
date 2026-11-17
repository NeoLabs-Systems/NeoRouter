'use strict';
exports.up = (db) => db.exec(`
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
`);
