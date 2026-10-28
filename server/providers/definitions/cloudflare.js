'use strict';

module.exports = {
  id: 'cloudflare',
  label: 'Cloudflare Workers AI',
  description: 'Serverless models on Cloudflare with a daily free allowance.',
  category: 'free',
  website: 'https://developers.cloudflare.com/workers-ai',
  keyUrl: 'https://dash.cloudflare.com/profile/api-tokens',
  adapter: 'cloudflare',
  baseUrl: 'https://api.cloudflare.com/client/v4/accounts/{accountId}/ai/v1',
  fields: [{ key: 'accountId', label: 'Account ID', required: true, placeholder: '32-character account id' }],
  billing: { defaultMode: 'free_tier', freeTierAll: true, note: 'Free only on the Workers Free plan (10,000 neurons a day, then requests fail). On Workers Paid the overage is billed: choose paid for such accounts.', source: 'https://developers.cloudflare.com/workers-ai/platform/pricing/', checkedAt: '2026-10-28' },
  limits: { pool: 'account', account: { rpm: 300 } },
  // Embeddings use the OpenAI-compatible layer; images and audio /ai/run.
  media: { embed: '/embeddings', generateImage: true, speech: true, transcribe: true },
};
