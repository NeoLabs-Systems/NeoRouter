'use strict';

// First-party paid APIs. Prices come from the catalog metadata sources
// because these providers do not publish prices in /models.
module.exports = [
  {
    id: 'openai',
    label: 'OpenAI',
    description: 'GPT and o-series models.',
    category: 'paid',
    website: 'https://openai.com',
    keyUrl: 'https://platform.openai.com/api-keys',
    adapter: 'openai_compatible',
    baseUrl: 'https://api.openai.com/v1',
    billing: { defaultMode: 'paid', note: 'Prepaid credits; no free models through the API.', source: 'https://developers.openai.com/api/docs/guides/rate-limits', checkedAt: '2026-11-07' },
    quirks: { maxTokensField: 'max_completion_tokens' },
    live: ['openai_live', 'openai_realtime'],
  },
  {
    id: 'anthropic',
    label: 'Anthropic',
    description: 'Claude models via the native Messages API.',
    category: 'paid',
    website: 'https://anthropic.com',
    keyUrl: 'https://platform.claude.com/settings/keys',
    adapter: 'anthropic',
    baseUrl: 'https://api.anthropic.com/v1',
    billing: { defaultMode: 'paid', note: 'Prepaid credits; no free tier.', source: 'https://platform.claude.com/docs/en/api/rate-limits', checkedAt: '2026-11-07' },
  },
  {
    id: 'xai',
    label: 'xAI',
    description: 'Grok models.',
    category: 'paid',
    website: 'https://x.ai',
    keyUrl: 'https://console.x.ai',
    adapter: 'openai_compatible',
    baseUrl: 'https://api.x.ai/v1',
    billing: { defaultMode: 'paid', note: 'Prepaid credits; requests are refused once they run out (unless invoiced billing is on).', source: 'https://docs.x.ai/docs/key-information/billing', checkedAt: '2026-11-07' },
  },
  {
    id: 'deepseek',
    label: 'DeepSeek',
    description: 'DeepSeek chat and reasoner models.',
    category: 'paid',
    website: 'https://deepseek.com',
    keyUrl: 'https://platform.deepseek.com/api_keys',
    adapter: 'openai_compatible',
    baseUrl: 'https://api.deepseek.com/v1',
    billing: { defaultMode: 'paid', note: 'Prepaid top-up; no free models.', source: 'https://api-docs.deepseek.com/quick_start/pricing', checkedAt: '2026-11-07' },
    quota: {
      probe: {
        path: 'https://api.deepseek.com/user/balance',
        parse: (data) => (data?.balance_infos || [])
          .filter((info) => info.currency === 'USD')
          .map((info) => ({ metric: 'balance', limit: null, remaining: Number(info.total_balance) })),
      },
    },
  },
  {
    id: 'moonshot',
    label: 'Moonshot AI',
    description: 'Kimi models.',
    category: 'paid',
    website: 'https://moonshot.ai',
    keyUrl: 'https://platform.kimi.ai/console/api-keys',
    adapter: 'openai_compatible',
    baseUrl: 'https://api.moonshot.ai/v1',
    billing: { defaultMode: 'paid', note: 'Prepaid; a 1 USD recharge activates the API.', source: 'https://platform.kimi.ai/docs/guide/start-using-kimi-api', checkedAt: '2026-11-07' },
  },
  {
    id: 'minimax',
    label: 'MiniMax',
    description: 'MiniMax text models.',
    category: 'paid',
    website: 'https://www.minimax.io',
    keyUrl: 'https://platform.minimax.io/user-center/basic-information/interface-key',
    adapter: 'openai_compatible',
    baseUrl: 'https://api.minimax.io/v1',
    billing: { defaultMode: 'paid', note: 'Pay as you go from balance. M Plan subscription keys are separate (see MiniMax M Plan).', source: 'https://platform.minimax.io/docs/guides/pricing-paygo', checkedAt: '2026-11-07' },
  },
  {
    id: 'dashscope',
    label: 'Alibaba Model Studio',
    description: 'Qwen models (international DashScope endpoint).',
    category: 'paid',
    website: 'https://www.alibabacloud.com/product/modelstudio',
    keyUrl: 'https://modelstudio.console.alibabacloud.com',
    adapter: 'openai_compatible',
    baseUrl: 'https://dashscope-intl.aliyuncs.com/compatible-mode/v1',
    // The new-account quota is a one-time trial that bills by default afterwards.
    billing: { defaultMode: 'paid', note: 'New accounts get a one-time quota (about 1M tokens per model, 90 days). After that you are billed unless Free Quota Only is turned on per model in Model Studio.', source: 'https://www.alibabacloud.com/help/en/model-studio/new-free-quota', checkedAt: '2026-11-07' },
  },
];
