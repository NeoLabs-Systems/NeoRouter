'use strict';

// What a free request would have cost at the model's market price (median
// paid price of the same model across providers), for "saved" statistics.
exports.up = (db) => {
  db.exec(`ALTER TABLE requests ADD COLUMN saved_usd REAL NOT NULL DEFAULT 0;
    UPDATE requests SET saved_usd = COALESCE((
      SELECT (requests.input_tokens * COALESCE(md.price_in, 0) + requests.output_tokens * COALESCE(md.price_out, 0)) / 1000000.0
      FROM models m JOIN model_metadata md ON md.canonical_id = m.canonical_id WHERE m.id = requests.model_ref
    ), 0) WHERE is_free = 1 AND status = 'success';`);
};
