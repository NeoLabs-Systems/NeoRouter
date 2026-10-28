'use strict';

module.exports = {
  id: 'mistral',
  label: 'Mistral AI',
  description: 'Mistral, Codestral and Magistral models. The Experiment plan is free.',
  category: 'free',
  website: 'https://mistral.ai',
  keyUrl: 'https://console.mistral.ai/api-keys',
  adapter: 'openai_compatible',
  baseUrl: 'https://api.mistral.ai/v1',
  billing: { defaultMode: 'free_tier', freeTierAll: true, note: 'Free on the Experiment plan; the Scale plan is billed.' },
  limits: { pool: 'account', account: { rpm: 60, tpm: 500000 } },
  media: { embed: '/embeddings', moderate: '/moderations', transcribe: '/audio/transcriptions' },
};
