'use strict';

module.exports = {
  id: 'cerebras',
  label: 'Cerebras',
  description: 'Wafer-scale inference, very high tokens per second.',
  category: 'paid',
  website: 'https://cerebras.ai',
  keyUrl: 'https://cloud.cerebras.ai',
  adapter: 'openai_compatible',
  baseUrl: 'https://api.cerebras.ai/v1',
  // Since July 2026 there is no renewing free tier: new accounts get a $5
  // credit trial after adding a card, and usage is paid from credits.
  billing: {
    defaultMode: 'paid',
    note: 'No permanent free tier: a one-time $5 credit trial (card required), then prepaid credits.',
    source: 'https://inference-docs.cerebras.ai/support/rate-limits',
    checkedAt: '2026-10-28',
  },
  limits: { pool: 'model', defaults: { rpm: 30, rpd: 14400, tpm: 60000, tpd: 1000000 } },
};
