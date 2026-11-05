'use strict';

// Prepaid plans sold for use from any OpenAI-compatible tool. Each request
// costs nothing extra until the allowance runs out; the quota probe reports
// what is left so routing can use the plan first and stop in time.
// Checked on the providers' own pages, 2026-10-29.

// MiniMax reports per-model windows as percentages left. Its count fields are
// unreliable (often 0), so only the percentages are used.
function minimaxWindows(data) {
  const models = (Array.isArray(data?.model_remains) ? data.model_remains : [])
    .filter((item) => !/video|hailuo|speech|image|music/i.test(String(item.model_name || '')));
  const tightest = (percentField, endField) => {
    const values = models.map((item) => ({ value: Number(item[percentField]), resetAt: Number(item[endField]) || null }))
      .filter((item) => Number.isFinite(item.value));
    if (!values.length) return null;
    return values.reduce((low, item) => (item.value < low.value ? item : low));
  };
  const results = [];
  const shortWindow = tightest('current_interval_remaining_percent', 'end_time');
  const weekly = tightest('current_weekly_remaining_percent', 'weekly_end_time');
  if (shortWindow) results.push({ metric: 'plan_5h', limit: 100, remaining: shortWindow.value, resetAt: shortWindow.resetAt });
  if (weekly) results.push({ metric: 'plan_week', limit: 100, remaining: weekly.value, resetAt: weekly.resetAt });
  return results;
}

// OpenCode Go reports the console's three meters: the 5-hour block (rolling),
// the week and the month, each as the share used and when it resets.
// GET /usage -> { usage: { rolling|weekly|monthly: { status, percent, resetsAt } } }
function opencodeGoWindows(data) {
  const meters = data?.usage || data || {};
  const windows = [['rolling', 'plan_5h'], ['weekly', 'plan_week'], ['monthly', 'plan_month']];
  const results = [];
  for (const [name, metric] of windows) {
    const meter = meters[name];
    const used = Number(meter?.percent ?? meter?.usagePercent);
    if (!meter || !Number.isFinite(used)) continue;
    const resetAt = Date.parse(meter.resetsAt) || (Number(meter.resetInSec) ? Date.now() + Number(meter.resetInSec) * 1000 : null);
    results.push({ metric, limit: 100, remaining: Math.max(0, Math.min(100, Math.round(100 - used))), resetAt });
  }
  return results;
}

module.exports = [
  {
    id: 'chatgpt',
    label: 'ChatGPT',
    description: 'Your ChatGPT plan (Plus, Pro) through the Codex sign-in.',
    category: 'plan',
    website: 'https://chatgpt.com/codex',
    adapter: 'codex',
    connect: 'codex_device',
    keyOptional: true,
    // Overridable for tests only.
    baseUrl: process.env.NEOROUTER_CODEX_BASE_URL || 'https://chatgpt.com/backend-api/codex',
    billing: {
      defaultMode: 'plan',
      plan: { note: 'Uses your ChatGPT plan: a 5-hour and a weekly window, which ChatGPT reports with every answer.' },
      note: 'Signs in the way Codex, OpenClaw and OpenCode do. Meant for your own plan: Team, Business and Enterprise workspaces follow their admins\' rules, and sharing one plan between people is against the terms.',
      source: 'https://developers.openai.com/codex/auth',
      checkedAt: '2026-10-29',
    },
    quirks: { timeoutMs: 300000 },
  },
  {
    id: 'synthetic',
    label: 'Synthetic',
    description: 'Open models on a flat subscription, for any OpenAI-compatible tool.',
    category: 'plan',
    website: 'https://synthetic.new',
    keyUrl: 'https://synthetic.new/user-settings/api',
    adapter: 'openai_compatible',
    baseUrl: 'https://api.synthetic.new/openai/v1',
    billing: {
      defaultMode: 'plan',
      plan: { note: 'Subscription (Go, Standard, Big) with a request allowance that renews on a schedule; NeoRouter reads what is left from Synthetic.' },
      note: 'A subscription: requests within the allowance cost nothing extra.',
      source: 'https://synthetic.new/pricing',
      checkedAt: '2026-10-29',
    },
    quota: {
      probe: {
        path: 'https://api.synthetic.new/v2/quotas',
        parse: (data) => {
          const plan = data?.subscription;
          if (!plan || !Number.isFinite(Number(plan.limit))) return [];
          return [{ metric: 'plan_requests', limit: Number(plan.limit), remaining: Math.max(0, Number(plan.limit) - Number(plan.requests || 0)), resetAt: Date.parse(plan.renewsAt) || null }];
        },
      },
    },
  },
  {
    id: 'minimax_plan',
    label: 'MiniMax M Plan',
    description: 'The MiniMax subscription (Go, Explore, Build) with its own plan key.',
    category: 'plan',
    website: 'https://www.minimax.io',
    keyUrl: 'https://platform.minimax.io/user-center/basic-information/interface-key',
    adapter: 'openai_compatible',
    baseUrl: 'https://api.minimax.io/v1',
    // The plan key does not list models; these are the plan's text models.
    models: { static: [{ id: 'MiniMax-M3.1-Flash', name: 'MiniMax M3.1 Flash', context_length: 1000000 }] },
    validate: false,
    billing: {
      defaultMode: 'plan',
      plan: { note: 'Usage windows of 5 hours and a week; MiniMax reports what is left of each.' },
      note: 'The M Plan key works in any OpenAI- or Anthropic-compatible tool; usage from all tools counts against the same windows.',
      source: 'https://platform.minimax.io/docs/coding-plan/faq',
      checkedAt: '2026-10-29',
    },
    quota: { probe: { path: 'https://www.minimax.io/v1/token_plan/remains', parse: minimaxWindows } },
  },
  {
    id: 'kimi_code',
    label: 'Kimi Code',
    description: 'The Kimi membership coding plan with its own API key.',
    category: 'plan',
    website: 'https://www.kimi.com/coding',
    keyUrl: 'https://www.kimi.com/code/console',
    adapter: 'openai_compatible',
    baseUrl: 'https://api.kimi.ai/coding/v1',
    // Documented plan models; which ones work depends on the membership tier.
    models: {
      static: [
        { id: 'k3.1-pro', name: 'Kimi K3.1 Pro', reasoning: true },
        { id: 'k3.1-pro-highspeed', name: 'Kimi K3.1 Pro HighSpeed', reasoning: true },
        { id: 'k3', name: 'Kimi K3', context_length: 1000000 },
        { id: 'k3-256k', name: 'Kimi K3 256K', context_length: 256000 },
        { id: 'kimi-for-coding', name: 'Kimi for Coding', context_length: 1000000 },
        { id: 'kimi-for-coding-highspeed', name: 'Kimi for Coding HighSpeed' },
      ],
    },
    billing: {
      defaultMode: 'plan',
      plan: { note: 'Usage windows of 5 hours and a week; which models you get depends on your membership.' },
      note: 'Kimi lists the tools it supports and forbids disguising the client; NeoRouter identifies itself honestly, so Kimi may refuse it. Check the membership terms before relying on it.',
      source: 'https://www.kimi.com/coding/docs/en/',
      checkedAt: '2026-10-29',
    },
    // Never pretend to be another client: Kimi treats a faked User-Agent as a violation.
    quirks: { userAgent: 'NeoRouter' },
  },
  {
    id: 'opencode_go',
    label: 'OpenCode Go',
    description: 'The OpenCode Go subscription: open and partner models for coding agents, with monthly usage limits.',
    category: 'plan',
    website: 'https://opencode.ai/go',
    adapter: 'openai_compatible',
    baseUrl: 'https://opencode.ai/inference/go/v1',
    // Without a session id Go refuses the request ("MissingSessionID"). It wants
    // the conversation's own id (for routing and prompt caching) and a client name.
    sessionHeader: 'x-opencode-session',
    quirks: { userAgent: 'NeoRouter' },
    quota: { probe: { path: '/usage', parse: opencodeGoWindows } },
    // The "Contributor" models are cheaper or free in exchange for training on
    // the prompts, so they start switched off.
    models: { optIn: ['contributor'] },
    // /models is public: the key and the subscription are checked with a 1-token request.
    validate: { chatProbe: true, probeModels: ['glm-5.3-flash'] },
    billing: {
      defaultMode: 'plan',
      plan: { note: 'A monthly dollar limit per model, with a 5-hour window (20% of it) and a weekly one (50%). NeoRouter reads the three windows from the OpenCode usage endpoint. With Extra Usage on, OpenCode carries on from your balance.' },
      note: 'Needs the Go subscription ($10 a month, Go Plus $40). Some models (DeepSeek among them) need the workspace Privacy setting on Global regions. Muse Spark Contributor and Iceman Contributor train on your prompts, so they start switched off. Grok, GPT and Claude Haiku keep requests for 30 days; most others keep none.',
      source: 'https://opencode.ai/docs/go/',
      checkedAt: '2026-11-04',
    },
  },
];

module.exports.minimaxWindows = minimaxWindows;
module.exports.opencodeGoWindows = opencodeGoWindows;
