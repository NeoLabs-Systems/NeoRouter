'use strict';

module.exports = {
  id: 'nvidia',
  label: 'NVIDIA NIM',
  description: 'Hosted NIM endpoints on build.nvidia.com for developers.',
  category: 'free',
  website: 'https://build.nvidia.com',
  keyUrl: 'https://build.nvidia.com/settings/api-keys',
  adapter: 'openai_compatible',
  baseUrl: 'https://integrate.api.nvidia.com/v1',
  // build.nvidia.com has no payment method at all: usage draws from free
  // trial credits (1,000, up to 5,000), so it can never be charged.
  billing: {
    defaultMode: 'free_tier',
    freeTierAll: true,
    note: 'Free trial credits (1,000, up to 5,000 on request) with no way to attach a payment method; when they run out requests fail instead of costing money.',
    source: 'https://forums.developer.nvidia.com/t/nvidia-nim-faq/300317',
    checkedAt: '2026-10-28',
  },
  limits: { pool: 'account', account: { rpm: 40 } },
  quirks: { singleToolCall: true },
};
