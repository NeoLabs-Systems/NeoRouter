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
  billing: { defaultMode: 'paid', note: 'No free tier: usage is paid from purchased credits (PRO accounts get $2 a month).', source: 'https://huggingface.co/docs/inference-providers/pricing', checkedAt: '2026-10-28' },
};
