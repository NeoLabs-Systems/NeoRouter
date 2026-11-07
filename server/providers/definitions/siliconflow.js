'use strict';

module.exports = {
  id: 'siliconflow',
  label: 'SiliconFlow',
  description: 'Open model hosting; new accounts get a small trial credit.',
  category: 'aggregator',
  website: 'https://siliconflow.com',
  keyUrl: 'https://cloud.siliconflow.com/account/ak',
  adapter: 'openai_compatible',
  baseUrl: 'https://api.siliconflow.com/v1',
  models: { path: '/models?type=text&sub_type=chat' },
  validate: { path: '/user/info' },
  billing: { defaultMode: 'paid', note: 'Balance-based. New accounts get about 1 USD of trial credit; the international service has no permanently free models.', source: 'https://siliconflow.com/pricing', checkedAt: '2026-11-07' },
  quota: {
    probe: {
      path: '/user/info',
      parse: (data) => {
        const balance = Number(data?.data?.totalBalance ?? data?.data?.balance);
        return Number.isFinite(balance) ? [{ metric: 'balance', limit: null, remaining: balance }] : [];
      },
    },
  },
};
