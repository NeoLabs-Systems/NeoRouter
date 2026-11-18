'use strict';

module.exports = {
  id: 'modelscope',
  label: 'ModelScope',
  description: 'Free API-Inference for open models (ModelScope account required).',
  category: 'free',
  website: 'https://modelscope.cn',
  keyUrl: 'https://modelscope.cn/my/myaccesstoken',
  adapter: 'openai_compatible',
  baseUrl: 'https://api-inference.modelscope.cn/v1',
  // /models answers 200 for any token, so validate with a 1-token chat.
  validate: { chatProbe: true },
  billing: { defaultMode: 'free_tier', freeTierAll: true, note: 'Free daily points (about 250 calls to mainstream models a day); needs an Alibaba Cloud account with real-name verification. Points cannot be bought, so nothing is charged.', source: 'https://www.modelscope.cn/docs/model-service/API-Inference/limits', checkedAt: '2026-11-07' },
  limits: { pool: 'account', account: { rpd: 250 } },
};
