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
    note: 'No permanent free tier: a 5 USD trial credit (verified payment method required, expires after 30 days), then prepaid credits.',
    source: 'https://inference-docs.cerebras.ai/support/rate-limits',
    checkedAt: '2026-11-07',
  },
  // Trial limits; pay-as-you-go accounts get far more and learn them from headers.
  limits: { pool: 'model', defaults: { rpm: 5, tpm: 30000, tpd: 1000000 } },
};
