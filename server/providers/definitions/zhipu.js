'use strict';

module.exports = [
  {
    id: 'zai',
    label: 'Z.ai (GLM)',
    description: 'GLM models from Zhipu, international endpoint. Flash models are free.',
    category: 'free',
    website: 'https://z.ai',
    keyUrl: 'https://z.ai/manage-apikey/apikey-list',
    adapter: 'openai_compatible',
    baseUrl: 'https://api.z.ai/api/paas/v4',
    billing: { defaultMode: 'paid', alwaysFree: ['-flash$'], note: 'GLM-*-Flash models are free; other models need balance.' },
  },
  {
    id: 'zhipu',
    label: 'Zhipu BigModel',
    description: 'GLM models from Zhipu, mainland China endpoint. Flash models are free.',
    category: 'free',
    website: 'https://open.bigmodel.cn',
    keyUrl: 'https://open.bigmodel.cn/usercenter/apikeys',
    adapter: 'openai_compatible',
    baseUrl: 'https://open.bigmodel.cn/api/paas/v4',
    billing: { defaultMode: 'paid', alwaysFree: ['-flash$'], note: 'GLM-*-Flash models are free; other models need balance.' },
  },
];
