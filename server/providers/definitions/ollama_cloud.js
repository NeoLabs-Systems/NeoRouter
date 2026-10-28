'use strict';

module.exports = {
  id: 'ollama_cloud',
  label: 'Ollama Cloud',
  description: 'Large open models hosted by Ollama.',
  category: 'free',
  website: 'https://ollama.com',
  keyUrl: 'https://ollama.com/settings/keys',
  adapter: 'openai_compatible',
  baseUrl: 'https://ollama.com/v1',
  billing: { defaultMode: 'free_tier', freeTierAll: true, note: 'Free plan with hourly and weekly usage limits.' },
  quirks: { timeoutMs: 180000 },
};
