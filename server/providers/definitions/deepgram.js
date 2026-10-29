'use strict';

module.exports = {
  id: 'deepgram',
  label: 'Deepgram',
  description: 'Nova speech-to-text and Aura voices.',
  category: 'paid',
  website: 'https://deepgram.com',
  keyUrl: 'https://console.deepgram.com/project/default/keys',
  adapter: 'deepgram',
  baseUrl: 'https://api.deepgram.com/v1',
  validate: { path: '/projects' },
  media: { speech: true, transcribe: true },
  // Prices are pay-as-you-go list prices, used only for accounts set to paid.
  models: {
    static: [
      { id: 'nova-3', name: 'Nova-3', kind: 'transcription', price_per_minute: 0.0077 },
      { id: 'nova-2', name: 'Nova-2', kind: 'transcription', price_per_minute: 0.0058 },
      { id: 'aura-2', name: 'Aura-2', kind: 'speech', price_per_mchar: 30 },
    ],
  },
  // A one-time 200 USD trial credit, then pay per use with a card: no free tier.
  billing: {
    defaultMode: 'paid',
    note: 'New projects get a one-time 200 USD trial credit; after that, usage is billed per minute and per character.',
    source: 'https://deepgram.com/pricing',
    checkedAt: '2026-10-29',
  },
  limits: { pool: 'account', account: { rpm: 100 } },
};
