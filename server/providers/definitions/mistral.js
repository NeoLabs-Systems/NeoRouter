'use strict';

module.exports = {
  id: 'mistral',
  label: 'Mistral AI',
  description: 'Mistral, Codestral and Magistral models.',
  category: 'paid',
  website: 'https://mistral.ai',
  keyUrl: 'https://console.mistral.ai/api-keys',
  adapter: 'openai_compatible',
  baseUrl: 'https://api.mistral.ai/v1',
  // Mistral's docs now say API keys need billing activation; free only if your workspace is still on the free Experiment plan.
  billing: { defaultMode: 'paid', freeTierAll: true, note: 'API use needs billing activation and is pay as you go; the free mode has no API access. Choose free only for an old Experiment workspace without a payment method.', source: 'https://docs.mistral.ai/admin/billing-usage/usage-limits', checkedAt: '2026-11-07' },
  limits: { pool: 'account', account: { rpm: 60, tpm: 500000 } },
  media: { embed: '/embeddings', moderate: '/moderations', transcribe: '/audio/transcriptions' },
  quirks: { embeddingDimensionsField: 'output_dimension' },
};
