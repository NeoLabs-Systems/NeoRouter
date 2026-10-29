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
    billing: { defaultMode: 'paid' },
    quirks: { maxTokensField: 'max_completion_tokens' },
  },
  {
    id: 'anthropic',
    label: 'Anthropic',
    description: 'Claude models via the native Messages API.',
    category: 'paid',
    website: 'https://anthropic.com',
    keyUrl: 'https://console.anthropic.com/settings/keys',
    adapter: 'anthropic',
    baseUrl: 'https://api.anthropic.com/v1',
    billing: {
      defaultMode: 'paid',
      // Claude subscriptions cannot be used from other tools (Anthropic blocks
      // subscription sign-ins outside Claude Code); API credits that come with
      // a plan can, through a normal Console key.
      plan: { note: 'For monthly API credits that come with a Claude plan: use a Console API key from the linked organization. A Claude Pro/Max sign-in itself only works in Claude Code.' },
    },
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
    billing: { defaultMode: 'paid' },
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
    billing: { defaultMode: 'paid' },
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
    keyUrl: 'https://platform.moonshot.ai/console/api-keys',
    adapter: 'openai_compatible',
    baseUrl: 'https://api.moonshot.ai/v1',
    billing: { defaultMode: 'paid' },
  },
  {
    id: 'minimax',
    label: 'MiniMax',
    description: 'MiniMax text models.',
    category: 'paid',
    website: 'https://www.minimax.io',
    keyUrl: 'https://www.minimax.io/platform/user-center/basic-information/interface-key',
    adapter: 'openai_compatible',
    baseUrl: 'https://api.minimax.io/v1',
    billing: { defaultMode: 'paid' },
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
    billing: { defaultMode: 'paid' },
  },
];
