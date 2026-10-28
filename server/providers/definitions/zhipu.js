'use strict';

// Free models only by exact id: "-flash" alone also matches paid models.
const ZAI_FREE = ['^glm-4\\.7-flash$', '^glm-4\\.5-flash$', '^glm-4\\.6v-flash$'];

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
    billing: { defaultMode: 'paid', alwaysFree: ZAI_FREE, note: 'Only GLM-4.7-Flash, GLM-4.5-Flash and GLM-4.6V-Flash are free; newer Flash models such as GLM-5.3-Flash are paid.', source: 'https://docs.z.ai/guides/overview/pricing', checkedAt: '2026-10-28' },
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
    billing: { defaultMode: 'paid', alwaysFree: ZAI_FREE, note: 'Only GLM-4.7-Flash, GLM-4.5-Flash and GLM-4.6V-Flash are free; newer Flash models such as GLM-5.3-Flash are paid.', source: 'https://docs.z.ai/guides/overview/pricing', checkedAt: '2026-10-28' },
  },
];
