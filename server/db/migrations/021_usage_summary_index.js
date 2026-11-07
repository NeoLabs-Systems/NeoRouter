'use strict';

// The usage summary (home page, activity) reads counts, costs and tokens over
// up to a month of requests. Each request row also carries its trail, so
// scanning the table itself read far more than needed (2 s for a busy week).
// This index holds every column the summary reads, so it is answered from the
// index alone (about 0.1 s for the same week).
exports.up = (db) => {
  db.exec(`CREATE INDEX IF NOT EXISTS requests_user_time_summary ON requests
    (user_id, created_at, status, is_free, attempts, cost_usd, saved_usd, input_tokens, output_tokens, latency_ms, ttft_ms, router_key_id, model_label, error_code)`);
};

exports.down = (db) => {
  db.exec('DROP INDEX IF EXISTS requests_user_time_summary;');
};
