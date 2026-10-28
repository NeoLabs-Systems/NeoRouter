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
  billing: { defaultMode: 'free_tier', freeTierAll: true, note: '10,000 neurons per day free on the Workers Free plan.' },
  limits: { pool: 'account', account: { rpm: 300 } },
};
