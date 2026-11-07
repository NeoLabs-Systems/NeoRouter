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
  // Some models need Workers Paid and are never free (Kimi K2.6/K2.7, GLM-5.2/5.3, DeepSeek V4).
  billing: {
    defaultMode: 'free_tier',
    freeTierModels: ['^(?!(@cf/)?(moonshotai/kimi-k2\\.(6|7)|zai-org/glm-5\\.[23]|deepseek-ai/deepseek-v4)).*$'],
    note: 'Free only on the Workers Free plan: 10,000 neurons a day, then requests fail. Some models (Kimi K2.6/K2.7, GLM-5.2/5.3, DeepSeek V4) need Workers Paid and are never free. On Workers Paid the overage is billed: choose paid for such accounts.',
    source: 'https://developers.cloudflare.com/workers-ai/platform/pricing/',
    checkedAt: '2026-11-07',
  },
  limits: { pool: 'account', account: { rpm: 300 }, models: [{ match: 'kimi-k2\\.[67]|glm-5\\.[23]|deepseek-v4', rpm: 20 }] },
  // Embeddings use the OpenAI-compatible layer; images and audio /ai/run.
  media: { embed: '/embeddings', generateImage: true, speech: true, transcribe: true },
};
