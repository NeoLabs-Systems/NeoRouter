'use strict';

module.exports = {
  id: 'google',
  label: 'Google Gemini',
  description: 'Gemini and Gemma models through the Gemini API (AI Studio).',
  category: 'free',
  website: 'https://ai.google.dev',
  keyUrl: 'https://aistudio.google.com/apikey',
  adapter: 'google_gemini',
  baseUrl: 'https://generativelanguage.googleapis.com/v1beta',
  billing: {
    defaultMode: 'free_tier',
    // Only these models have a free tier; Pro previews, image, Omni and Lyria
    // models are paid-only. A model refused with "free_tier … limit: 0" is
    // learned as not free.
    freeTierModels: [
      '^gemini-(2\\.5|3|3\\.\\d)-flash(-lite)?(-preview)?$',
      '^gemini-flash(-lite)?-latest$',
      '^gemini-2\\.5-pro$',
      '^gemma-',
      // Embeddings, Flash speech and the transcribe models have a free tier
      // too (checked live on a project without billing, 2026-10-28).
      '^gemini-embedding-(001|2)(-preview)?$',
      '^gemini-(2\\.5|3\\.\\d)-flash(-lite)?(-preview)?-tts(-preview)?$',
      '^gemini-\\d+(\\.\\d+)?-transcribe$',
    ],
    note: 'Free only while the Google Cloud project behind the key has no billing account; with billing enabled every request is charged. NeoRouter verifies this automatically and only treats models with a free tier as free.',
    verify: { type: 'paid_model_probe', paidOnlyModels: ['^gemini-\\d+(\\.\\d+)?-pro-preview$', '^gemini-4-argon$', '^gemini-pro-latest$'] },
    source: 'https://ai.google.dev/gemini-api/docs/pricing',
    checkedAt: '2026-10-28',
  },
  limits: {
    pool: 'model',
    defaults: { rpm: 10, rpd: 250, tpm: 250000 },
    models: [
      { match: 'flash-lite', rpm: 15, rpd: 1000, tpm: 250000 },
      { match: '-pro', rpm: 5, rpd: 100, tpm: 250000 },
      { match: '^gemma', rpm: 30, rpd: 14400, tpm: 15000 },
      { match: 'embedding', rpm: 100, rpd: 1000, tpm: 30000 },
      { match: '-tts', rpm: 3, rpd: 15, tpm: 10000 },
      { match: 'transcribe', rpm: 10, rpd: 250, tpm: 250000 },
    ],
  },
};
