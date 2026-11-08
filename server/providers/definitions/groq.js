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
    checkedAt: '2026-11-07',
  },
  // Free plan limits (Llama 3.x and Kimi were retired for free accounts in 2026).
  limits: {
    pool: 'model',
    defaults: { rpm: 30, rpd: 1000, tpm: 8000, tpd: 200000 },
    models: [
      { match: 'safeguard', rpm: 3, rpd: 1000, tpm: 2000, tpd: 200000 },
      { match: 'gpt-oss', rpm: 30, rpd: 1000, tpm: 8000, tpd: 200000 },
      { match: 'qwen', rpm: 30, rpd: 1000, tpm: 8000, tpd: 200000 },
      { match: 'whisper', rpm: 20, rpd: 2000 },
      { match: 'orpheus', rpm: 10, rpd: 100, tpm: 1200, tpd: 3600 },
      { match: 'prompt-guard', rpm: 30, rpd: 14400, tpm: 15000, tpd: 500000 },
    ],
  },
  media: { transcribe: '/audio/transcriptions', speech: '/audio/speech' },
  // Orpheus voices; the model refuses OpenAI voice names.
  quirks: {
    defaultVoice: 'troy',
    voices: ['autumn', 'diana', 'hannah', 'austin', 'daniel', 'troy'],
    modelVoices: [{ match: 'orpheus-arabic', voices: ['abdullah', 'fahad', 'sultan', 'lulwa', 'noura', 'aisha'], defaultVoice: 'fahad' }],
    speechFormats: ['wav'],
    // Orpheus takes at most 200 characters per request.
    maxSpeechChars: 200,
  },
  // Groq reports requests per day and tokens per minute in the plain headers.
  quota: { headerPeriods: { requests: 'day', tokens: 'minute' } },
};
