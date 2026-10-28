'use strict';

module.exports = {
  id: 'huggingface',
  label: 'Hugging Face',
  description: 'Inference Providers router across many hosts.',
  category: 'free',
  website: 'https://huggingface.co',
  keyUrl: 'https://huggingface.co/settings/tokens',
  adapter: 'openai_compatible',
  baseUrl: 'https://router.huggingface.co/v1',
  validate: { path: 'https://huggingface.co/api/whoami-v2' },
  billing: { defaultMode: 'free_tier', freeTierAll: true, note: 'Monthly free credits; without billing, requests stop when they run out.' },
};
