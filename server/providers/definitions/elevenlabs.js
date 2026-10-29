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
    note: 'The Free plan has 10,000 credits a month without a card (about 10 minutes of speech) and requests fail beyond it; it is for non-commercial use with attribution. Paid plans bill overage: choose paid for such keys.',
    source: 'https://elevenlabs.io/pricing',
    checkedAt: '2026-10-29',
  },
  limits: { pool: 'account', account: { rpm: 60 } },
};
