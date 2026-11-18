'use strict';

// A partner in a pool (services/pools): another NeoRouter that runs our
// requests on its own free connections. Created by joining a pool, never by
// hand, so it is left out of "Add connection".
//
// - Everything it lists is free for us: the partner only shares free models.
// - Its rate-limit headers report what is left of our daily share.
// - It never receives who the end user is: identifying fields are dropped,
//   and no conversation id is forwarded (no sessionHeader).
// - X-NeoRouter-Hop marks the request as coming from a partner, so it is
//   never passed on to a third instance.
// - A partner without a public address has a relay:// base URL: requests go
//   through the pool's host, sealed for that partner (services/pools/relay).
module.exports = {
  id: 'neorouter_peer',
  label: 'Pool partner',
  description: 'Free capacity a pool partner shares with you.',
  category: 'pool',
  adapter: 'openai_compatible',
  editableBaseUrl: true,
  baseUrlSchemes: ['http', 'https', 'relay'],
  headers: { 'X-NeoRouter-Hop': '1' },
  billing: {
    defaultMode: 'free_tier',
    freeTierAll: true,
    note: 'Runs on a partner’s free connections.',
  },
  limits: { pool: 'account' },
  quota: { headerPeriods: { requests: 'day', tokens: 'day' } },
  quirks: {
    userAgent: 'NeoRouter-Pool',
    dropFields: ['user', 'metadata', 'safety_identifier', 'prompt_cache_key', 'store', 'service_tier'],
  },
};
