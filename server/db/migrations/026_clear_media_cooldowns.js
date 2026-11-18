'use strict';

// Speech, image and other media models were locked out for hours or days by
// refusals that were about one request, not the model: a voice another
// provider uses ("incompatible"), an ElevenLabs Voice Library voice ("payment
// required"), or a terms page since recognised as needing a person. Those
// requests are now handled per request; the old locks are lifted once, so each
// model gets one fresh try.
exports.up = (db) => {
  db.exec(`
    UPDATE model_stats SET cooldown_until = 0, cooldown_reason = NULL, cooldown_level = 0, breaker_open_until = 0
    WHERE cooldown_reason IN ('incompatible', 'payment_required', 'not_available')
      AND model_ref IN (SELECT id FROM models WHERE capability NOT IN ('chat', 'router', 'other'));
  `);
};

exports.down = () => {};
