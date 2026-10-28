'use strict';

module.exports = {
  id: 'ollama_cloud',
  label: 'Ollama Cloud',
  description: 'Large open models hosted by Ollama.',
  category: 'paid',
  website: 'https://ollama.com',
  keyUrl: 'https://ollama.com/settings/keys',
  adapter: 'openai_compatible',
  baseUrl: 'https://ollama.com/v1',
  // Cloud models are pay-per-token; the monthly free usage covers only this
  // list (Ollama settings → Usage, October 2026).
  billing: {
    defaultMode: 'free_tier',
    freeTierModels: [
      '^beam(:|$)',
      '^gemma4:31b',
      '^gpt-oss:(120b|20b)',
      '^nemotron-3-(nano:30b|super|ultra)',
    ],
    note: 'Only a few cloud models are covered by the free monthly usage; all others are pay-per-token. Keep the usage-credit balance at $0 with auto-reload off so nothing can be charged once the free usage is used up.',
    source: 'https://ollama.com/pricing',
    checkedAt: '2026-10-28',
  },
  quirks: { timeoutMs: 180000 },
};
