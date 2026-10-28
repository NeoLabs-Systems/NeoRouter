'use strict';

module.exports = {
  id: 'siliconflow',
  label: 'SiliconFlow',
  description: 'Open models with several permanently free variants.',
  category: 'free',
  website: 'https://siliconflow.com',
  keyUrl: 'https://cloud.siliconflow.com/account/ak',
  adapter: 'openai_compatible',
  baseUrl: 'https://api.siliconflow.com/v1',
  models: { path: '/models?type=text&sub_type=chat' },
  validate: { path: '/user/info' },
  billing: { defaultMode: 'paid', note: 'Models priced at 0 in the listing are free; the rest draw from balance.' },
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
