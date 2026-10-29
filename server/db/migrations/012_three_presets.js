'use strict';

// Routing presets were reduced to balanced, fast and smart; keys on an
// earlier preset move to the one it became (profiles.js ALIASES).
exports.up = (db) => {
  db.exec(`UPDATE router_keys SET profile = 'balanced' WHERE profile IN ('reliable', 'cheap');
    UPDATE router_keys SET profile = 'smart' WHERE profile = 'best';`);
};
