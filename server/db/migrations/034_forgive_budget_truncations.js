'use strict';

// Running out of a client's small max_tokens used to count against a
// reasoning model's reliability. It no longer does, so models whose record is
// only such truncations, never an answer, start from a clean record.
exports.up = (db) => {
  db.prepare(`UPDATE model_stats SET failures = 0, penalty = 0, recent_failures_json = '[]', cooldown_until = 0, cooldown_reason = NULL, cooldown_level = 0, breaker_open_until = 0
    WHERE successes = 0 AND last_error LIKE 'truncated:%' AND min_output_tokens > 0`).run();
};

exports.down = () => {};
