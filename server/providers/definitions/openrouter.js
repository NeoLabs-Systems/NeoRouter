'use strict';

module.exports = {
  id: 'openrouter',
  label: 'OpenRouter',
  description: 'Aggregator for hundreds of models. Models ending in :free cost nothing.',
  category: 'aggregator',
  website: 'https://openrouter.ai',
  keyUrl: 'https://openrouter.ai/keys',
  adapter: 'openai_compatible',
  baseUrl: 'https://openrouter.ai/api/v1',
  headers: { 'HTTP-Referer': 'https://github.com/NeoLabs-Systems/NeoRouter', 'X-Title': 'NeoRouter' },
  validate: { path: '/key' },
  billing: {
    defaultMode: 'paid',
    alwaysFree: [':free$'],
    note: 'Paid models draw from your credits. :free models share a daily request allowance (higher once 10 USD of credits were bought).',
  },
  limits: {
    pool: 'model',
    poolMatch: ':free$',
    account: { rpm: 20, rpd: 50 },
  },
  quota: {
    probe: {
      path: '/key',
      parse: (data) => {
        const info = data?.data || {};
        const results = [];
        if (info.limit !== null && info.limit !== undefined) {
          results.push({ metric: 'credits_usd', limit: Number(info.limit), remaining: Number(info.limit_remaining ?? info.limit - (info.usage || 0)) });
        }
        if (info.rate_limit?.requests) results.push({ metric: 'rate_requests', limit: Number(info.rate_limit.requests), remaining: null });
        return results;
      },
    },
  },
};
