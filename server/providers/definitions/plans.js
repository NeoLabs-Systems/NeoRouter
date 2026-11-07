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

// OpenCode Go reports its allowance as one percentage used per window on its
// usage endpoint (undocumented, but the one its own clients read):
// { usage: { rolling: { percent, resetsAt }, weekly: {...}, monthly: {...} } }.
// A window is reported as percentage used, so what is left is 100 - percent;
// older shapes with used/limit pairs are read too.
function opencodeGoWindows(data) {
  const usage = data?.usage && typeof data.usage === 'object' ? data.usage : data;
  const windows = [
    ['plan_5h', usage?.rolling],
    ['plan_week', usage?.weekly],
    ['plan_5h', usage?.rollingUsage ? { percent: Number(usage.rollingUsage.usagePercent), resetsAt: null, resetInSec: usage.rollingUsage.resetInSec } : null],
    ['plan_week', usage?.weeklyUsage ? { percent: Number(usage.weeklyUsage.usagePercent), resetsAt: null, resetInSec: usage.weeklyUsage.resetInSec } : null],
    ['plan_month', usage?.monthly],
  ];
  const seen = new Set();
  const results = [];
  for (const [metric, window] of windows) {
    if (!window || seen.has(metric)) continue;
    const used = Number(window.percent ?? window.usage_percent);
    const limit = Number(window.limit);
    const remainingDirect = Number(window.remaining ?? window.remaining_percent);
    let remaining = null;
    if (Number.isFinite(used)) remaining = Math.max(0, 100 - used);
    else if (Number.isFinite(remainingDirect)) remaining = remainingDirect;
    else if (Number.isFinite(limit) && Number.isFinite(Number(window.used))) remaining = Math.max(0, limit - Number(window.used));
    if (!Number.isFinite(remaining)) continue;
    const resetAt = Date.parse(window.resetsAt) || (Number(window.resetInSec) ? Date.now() + Number(window.resetInSec) * 1000 : null);
    seen.add(metric);
    results.push({ metric, limit: 100, remaining, resetAt });
  }
  return results;
}

module.exports = [
  {
    id: 'opencode_go',
    label: 'OpenCode Go',
    description: 'The OpenCode Go subscription (Go, Go Plus, Go Teams) for coding agents.',
    category: 'plan',
    website: 'https://opencode.ai/docs/go',
    keyUrl: 'https://opencode.ai/console',
    adapter: 'opencode_go',
    baseUrl: 'https://opencode.ai/inference/go',
    chatPath: '/openai/v1/chat/completions',
    // /models is public, so the authenticated usage route proves the key.
    models: { path: '/v1/models' },
    validate: { path: '/v1/usage' },
    billing: {
      defaultMode: 'plan',
      plan: { note: 'Each model has its own monthly allowance; 5 hours, a week and the month draw on it (20 %, 50 %, 100 %). Go reports what is left of each window.' },
      note: 'A coding subscription: requests within the allowance cost nothing extra. OpenCode asks clients to identify themselves and to send a stable session id, which NeoRouter does not fake. Extra Usage (falling back to a prepaid balance) is off unless it is turned on in the console, so a used-up window blocks requests. The free models Go includes for a limited time are read from its own listing.',
      source: 'https://opencode.ai/docs/go',
      checkedAt: '2026-11-07',
    },
    quota: { probe: { path: 'https://opencode.ai/inference/go/v1/usage', parse: opencodeGoWindows } },
    quirks: { timeoutMs: 300000, userAgent: 'NeoRouter' },
  },
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
];

module.exports.minimaxWindows = minimaxWindows;
module.exports.opencodeGoWindows = opencodeGoWindows;
