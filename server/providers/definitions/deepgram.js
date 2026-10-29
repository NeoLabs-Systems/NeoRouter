'use strict';

module.exports = {
  id: 'deepgram',
  label: 'Deepgram',
  description: 'Nova speech-to-text and Aura voices.',
  category: 'free',
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
  billing: {
    defaultMode: 'free_tier',
    freeTierAll: true,
    note: 'New projects get 200 USD of free credit without a card; when it is used up requests fail. With a card on file, usage beyond it is billed: choose paid for such keys.',
    source: 'https://deepgram.com/pricing',
    checkedAt: '2026-10-29',
  },
  limits: { pool: 'account', account: { rpm: 100 } },
};
