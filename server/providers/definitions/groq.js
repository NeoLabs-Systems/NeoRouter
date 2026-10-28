'use strict';

module.exports = {
  id: 'groq',
  label: 'Groq',
  description: 'LPU inference with a generous free tier for open models.',
  category: 'free',
  website: 'https://groq.com',
  keyUrl: 'https://console.groq.com/keys',
  adapter: 'openai_compatible',
  baseUrl: 'https://api.groq.com/openai/v1',
  billing: {
    defaultMode: 'free_tier',
    freeTierAll: true,
    note: 'Free tier applies while no payment method is on file in the Groq console.',
  },
  limits: {
    pool: 'model',
    defaults: { rpm: 30, rpd: 1000, tpm: 6000, tpd: 100000 },
    models: [
      { match: 'llama-3\\.1-8b-instant', rpm: 30, rpd: 14400, tpm: 6000, tpd: 500000 },
      { match: 'llama-3\\.3-70b', rpm: 30, rpd: 1000, tpm: 12000, tpd: 100000 },
      { match: 'gpt-oss', rpm: 30, rpd: 1000, tpm: 8000, tpd: 200000 },
      { match: 'qwen', rpm: 60, rpd: 1000, tpm: 6000, tpd: 500000 },
      { match: 'kimi', rpm: 60, rpd: 1000, tpm: 10000, tpd: 300000 },
    ],
  },
  // Groq reports requests per day and tokens per minute in the plain headers.
  quota: { headerPeriods: { requests: 'day', tokens: 'minute' } },
};
