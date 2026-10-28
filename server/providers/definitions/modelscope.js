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
  billing: { defaultMode: 'free_tier', freeTierAll: true, note: 'Daily free call allowance across models.' },
  limits: { pool: 'model', account: { rpd: 2000 }, defaults: { rpd: 500 } },
};
