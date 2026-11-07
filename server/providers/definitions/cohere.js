'use strict';

module.exports = {
  id: 'cohere',
  label: 'Cohere',
  description: 'Command models via the OpenAI compatibility API.',
  category: 'free',
  website: 'https://cohere.com',
  keyUrl: 'https://dashboard.cohere.com/api-keys',
  adapter: 'openai_compatible',
  baseUrl: 'https://api.cohere.ai/compatibility/v1',
  models: { path: 'https://api.cohere.com/v1/models?endpoint=chat&page_size=1000', extraPaths: ['https://api.cohere.com/v1/models?endpoint=embed&page_size=1000'] },
  media: { embed: '/embeddings' },
  validate: { path: 'https://api.cohere.com/v1/models?endpoint=chat&page_size=1' },
  billing: { defaultMode: 'free_tier', freeTierAll: true, note: 'Trial keys are free: 1,000 API calls a month and 20 chat requests a minute; requests fail beyond that. Production keys are billed per token.', source: 'https://docs.cohere.com/docs/rate-limits', checkedAt: '2026-11-07' },
  limits: { pool: 'account', account: { rpm: 20 } },
  quirks: { stripSchemaKeys: ['additionalProperties', '$schema'], streamUsage: false },
};
