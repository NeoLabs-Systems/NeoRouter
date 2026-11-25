'use strict';

// Plan facts live with the provider; model availability is discovered from
// the authenticated listing. Manual account limits cover founder bonuses.
module.exports = {
  id: 'kourier',
  label: 'Kourier',
  description: 'Flat-rate inference billed by simultaneous requests.',
  category: 'plan',
  website: 'https://kourier.sh',
  keyUrl: 'https://app.kourier.sh',
  adapter: 'openai_compatible',
  baseUrl: 'https://api.kourier.sh/v1',
  auth: { type: 'bearer' },
  models: {
    path: '/models',
    // The listing is bare (id/owned_by only), so the documented limits live
    // here: 256K total context, 16K output cap per response
    // (https://docs.kourier.sh/docs/models, checked 2026-11-20).
    facts: { 'deepseek-v4.1-flash': { contextWindow: 262144, maxOutput: 16384 } },
  },
  billing: {
    defaultMode: 'flat_rate',
    trustListingFree: false,
    flatRate: {
      defaultPlan: 'starter',
      plans: [{ id: 'starter', label: 'Starter', concurrent: 1 }, { id: 'pro', label: 'Pro', concurrent: 3 }],
    },
    plan: { note: 'Paid concurrency slots with no token or request-volume allowance. Choose your plan and edit the concurrent limit for bonuses.' },
    source: 'https://docs.kourier.sh/docs/models',
    checkedAt: '2026-11-16',
    note: 'Keys on the same Kourier account share the plan slots; extra keys do not add capacity.',
  },
  limits: { pool: 'account', account: { concurrent: 1 } },
};
