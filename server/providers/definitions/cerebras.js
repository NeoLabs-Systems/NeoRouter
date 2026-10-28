'use strict';

module.exports = {
  id: 'cerebras',
  label: 'Cerebras',
  description: 'Wafer-scale inference, very high tokens per second.',
  category: 'free',
  website: 'https://cerebras.ai',
  keyUrl: 'https://cloud.cerebras.ai',
  adapter: 'openai_compatible',
  baseUrl: 'https://api.cerebras.ai/v1',
  billing: { defaultMode: 'free_tier', freeTierAll: true, note: 'Free tier with daily token allowance.' },
  limits: { pool: 'model', defaults: { rpm: 30, rpd: 14400, tpm: 60000, tpd: 1000000 } },
};
