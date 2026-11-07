'use strict';

module.exports = {
  id: 'elevenlabs',
  label: 'ElevenLabs',
  description: 'Natural voices and Scribe speech-to-text.',
  category: 'free',
  website: 'https://elevenlabs.io',
  keyUrl: 'https://elevenlabs.io/app/settings/api-keys',
  adapter: 'elevenlabs',
  baseUrl: 'https://api.elevenlabs.io/v1',
  auth: { type: 'header', header: 'xi-api-key' },
  validate: { path: '/models' },
  media: { speech: true, transcribe: true },
  // Scribe is not in /v1/models.
  extraModels: [{ id: 'scribe_v2', name: 'Scribe v2', type: 'transcription' }],
  billing: {
    defaultMode: 'free_tier',
    freeTierAll: true,
    note: 'Free plan: 10,000 credits a month, non-commercial with attribution, default voices only (library voices need a paid plan). Requests fail beyond it unless a pay-as-you-go balance was topped up, which is then spent: choose paid for paid-plan keys or accounts with a top-up balance.',
    source: 'https://elevenlabs.io/pricing',
    checkedAt: '2026-11-07',
  },
  limits: { pool: 'account', account: { concurrent: 2 } },
};
