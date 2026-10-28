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
    note: 'Free while the Groq organisation is on the Free plan (no payment method). NeoRouter checks the plan limits on every response and stops treating models as free as soon as the account is on a paid plan.',
    // Free-plan daily request limits are at most 14,400; paid plans are higher.
    verify: { type: 'ratelimit_headers', maxFreeRequestsPerDay: 14400, probeModels: ['gpt-oss-20b', 'llama', 'prompt-guard'] },
    source: 'https://console.groq.com/docs/billing-faqs',
    checkedAt: '2026-10-28',
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
      { match: 'whisper', rpm: 20, rpd: 2000 },
      { match: 'orpheus', rpm: 10, rpd: 100, tpm: 1200, tpd: 3600 },
      { match: 'prompt-guard', rpm: 30, rpd: 14400, tpm: 15000, tpd: 500000 },
    ],
  },
  media: { transcribe: '/audio/transcriptions', speech: '/audio/speech' },
  // Orpheus voices; the model refuses OpenAI voice names.
  quirks: { defaultVoice: 'troy', voices: ['autumn', 'diana', 'hannah', 'austin', 'daniel', 'troy'], speechFormats: ['wav'] },
  // Groq reports requests per day and tokens per minute in the plain headers.
  quota: { headerPeriods: { requests: 'day', tokens: 'minute' } },
};
