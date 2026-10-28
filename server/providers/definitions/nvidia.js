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
  billing: { defaultMode: 'free_tier', freeTierAll: true, note: 'Free developer access with a per-minute request cap.' },
  limits: { pool: 'account', account: { rpm: 40 } },
  quirks: { singleToolCall: true },
};
