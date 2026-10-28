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
    freeTierAll: true,
    note: 'Free while the Google Cloud project behind the key has no billing account. Some models have no free quota and are learned as unavailable.',
  },
  limits: {
    pool: 'model',
    defaults: { rpm: 10, rpd: 250, tpm: 250000 },
    models: [
      { match: 'flash-lite', rpm: 15, rpd: 1000, tpm: 250000 },
      { match: '-pro', rpm: 5, rpd: 100, tpm: 250000 },
      { match: '^gemma', rpm: 30, rpd: 14400, tpm: 15000 },
    ],
  },
};
