'use strict';

// Smaller OpenAI-compatible hosts and gateways: full entries like every other
// provider, each with what it is, where to get a key, and what is really free.
// Facts were checked on each provider's own site and live model list on the
// date in `checkedAt`; free offers here change often, so the defaults lean
// to "paid" wherever a balance or card could be charged.
//
// Where a provider marks its free models in the listing (is_free, tier,
// access_tier, billing_mode), model_info.js reads that directly, so no name
// pattern is needed and the list can change without a code change.
const BROWSER_UA = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36';
const CHECKED = '2026-11-07';

/** One OpenAI-compatible provider. `category`, `description` and `billing` are required. */
function provider(id, label, baseUrl, { category, description, billing, ...rest }) {
  if (!category || !description || !billing) throw new Error(`Provider ${id} needs a category, a description and billing facts.`);
  return {
    id,
    label,
    description,
    category,
    adapter: 'openai_compatible',
    baseUrl,
    ...rest,
    billing: { defaultMode: 'paid', checkedAt: CHECKED, ...billing },
  };
}

module.exports = [
  // ----------------------------------------------------------------- free tiers and regional hosts
  provider('sealion', 'SEA-LION', 'https://api.sea-lion.ai/v1', {
    category: 'free', description: 'AI Singapore’s Southeast Asian language models; a free trial key, 10 requests a minute.', website: 'https://sea-lion.ai', keyUrl: 'https://playground.sea-lion.ai',
    billing: { defaultMode: 'free_tier', freeTierAll: true, note: 'Free trial key, one per person; there is no billing.', source: 'https://docs.sea-lion.ai/guides/inferencing/api' },
    limits: { pool: 'account', account: { rpm: 10 } },
  }),
  provider('typhoon', 'Typhoon', 'https://api.opentyphoon.ai/v1', {
    category: 'free', description: 'Thai language models from SCB 10X; a free research API.', website: 'https://opentyphoon.ai', keyUrl: 'https://playground.opentyphoon.ai/api-key',
    billing: { defaultMode: 'free_tier', freeTierAll: true, note: 'Free research API; usage data is collected to improve the models.', source: 'https://docs.opentyphoon.ai/th/faq' },
    limits: { defaults: { rpm: 200 }, models: [{ match: '^typhoon-ocr', rpm: 20 }, { match: 'asr', rpm: 100 }] },
  }),
  provider('longcat', 'LongCat', 'https://api.longcat.chat/openai/v1', {
    category: 'paid', description: 'Meituan’s LongCat models; prepaid, no free tier.', website: 'https://longcat.chat', keyUrl: 'https://longcat.chat/platform/api_keys',
    billing: { note: 'Prepaid balance or 30-day token packs; requests are refused at 0 USD.', source: 'https://longcat.chat/platform/docs/api-pay-as-you-go' },
  }),
  provider('electronhub', 'Electron Hub', 'https://api.electronhub.ai/v1', {
    category: 'aggregator', description: 'About 600 models on one key; models ending in :free cost nothing, about 10 a day.', website: 'https://electronhub.ai', keyUrl: 'https://app.electronhub.ai',
    validate: { path: '/user/me' },
    billing: { alwaysFree: [':free$'], note: 'Models ending in :free use daily "Neutrinos", not money: 10 a day (reset 21:00 UTC), failed requests count too, and phone verification is needed. Other models use plan credits or a prepaid balance.', source: 'https://docs.electronhub.ai/billing/credits' },
    limits: { pool: 'account', account: { rpm: 5 }, models: [{ match: ':free$', rpd: 10 }] },
  }),
  provider('kenari', 'Kenari', 'https://kenari.id/v1', {
    category: 'aggregator', description: 'Indonesian gateway priced in rupiah; models ending in :free cost nothing.', website: 'https://kenari.id', keyUrl: 'https://kenari.id/keys',
    validate: { path: '/account/balance' },
    billing: { alwaysFree: [':free$'], note: 'Models ending in :free: 50 a day, 5 a minute (1,000 a day after a Rp 10.000 top-up). Others use a prepaid rupiah balance.', source: 'https://kenari.id/api/public/pricing' },
    limits: { pool: 'account', poolMatch: ':free$', account: { rpm: 5, rpd: 50 } },
  }),
  // Without X-Free-Fallback: false, requests past the free cap are billed from the balance.
  provider('bazaarlink', 'BazaarLink', 'https://api.bazaarlink.ai/v1', {
    category: 'aggregator', description: 'Taiwanese gateway; models ending in :free cost nothing within a daily cap.', website: 'https://bazaarlink.ai', keyUrl: 'https://bazaarlink.ai/keys',
    headers: { 'X-Free-Fallback': 'false' },
    billing: { alwaysFree: [':free$'], note: 'Models ending in :free: 10 a minute, 60 a day (120 after a top-up). NeoRouter tells it to refuse past the cap instead of billing. Others use a prepaid balance.', source: 'https://bazaarlink.ai/docs#free-models' },
    limits: { pool: 'account', account: { rpm: 20 }, models: [{ match: ':free$', rpm: 10, rpd: 60 }] },
  }),
  provider('reka', 'Reka', 'https://api.reka.ai/v1', {
    category: 'paid', description: 'Reka’s own models plus open models; prepaid.', website: 'https://reka.ai', keyUrl: 'https://developer.reka.ai/keys',
    billing: { note: 'Prepaid credits, no free tier; requests fail at 0 USD.', source: 'https://docs.reka.ai/pricing' },
  }),
  provider('qianfan', 'Baidu Qianfan', 'https://qianfan.baidubce.com/v2', {
    category: 'paid', description: 'Baidu’s ERNIE and hosted open models, billed in CNY.', website: 'https://cloud.baidu.com/product-s/qianfan_home', keyUrl: 'https://console.bce.baidu.com/iam/#/iam/apikey/list',
    billing: { note: 'Billed in CNY after use, so the account can run into debt. No free models confirmed.', source: 'https://cloud.baidu.com/doc/qianfan/s/wmh4sv6ya' },
  }),
  provider('volcengine', 'Volcengine Ark', 'https://ark.cn-beijing.volces.com/api/v3', {
    category: 'paid', description: 'ByteDance’s Doubao and hosted open models, billed in CNY.', website: 'https://www.volcengine.com/product/ark', keyUrl: 'https://console.volcengine.com/ark/region:ark+cn-beijing/apiKey',
    billing: { note: 'One-time free tokens per model at sign-up (safe-trial mode stops there). With that mode off, use is billed in CNY after the fact.', source: 'https://www.volcengine.com/docs/82379/1465347' },
  }),
  provider('xfyun', 'iFlytek Spark', 'https://spark-api-open.xf-yun.com/v1', {
    category: 'paid', description: 'iFlytek’s Spark models; Spark Lite is free.', website: 'https://xinghuo.xfyun.cn', keyUrl: 'https://console.xfyun.cn/services/cbm',
    billing: { freeTierModels: ['^lite$'], note: 'Spark Lite is free with daily caps; other models need bought quota. Each key works for one model only.', source: 'https://www.xfyun.cn/doc/spark/HTTP%E8%B0%83%E7%94%A8%E6%96%87%E6%A1%A3.html' },
  }),
  provider('bai', 'B.AI', 'https://api.b.ai/v1', {
    category: 'aggregator', description: 'Gateway to Claude, GPT, Gemini, DeepSeek and more; prepaid credits.', website: 'https://b.ai', keyUrl: 'https://chat.b.ai/key',
    validate: { path: 'https://api.b.ai/balance' },
    billing: { note: 'Prepaid credits, no free models.', source: 'https://docs.b.ai/llmservice/faq/' },
  }),
  // Pay-as-you-go accounts are charged for overage, so "paid" unless the person is on the free plan.
  provider('anyapi', 'AnyAPI', 'https://api.anyapi.ai/v1', {
    category: 'aggregator', description: 'Gateway with a small daily free allowance on its free models.', website: 'https://anyapi.ai', keyUrl: 'https://dash.anyapi.ai',
    billing: { freeTierModels: [':free$'], note: 'Free plan without a card: free models only, about 100K tokens a day (the cap may be shared). On pay as you go, overage is charged automatically.', source: 'https://anyapi.ai/pricing' },
  }),
  provider('radeon', 'AMD Radeon Cloud', 'https://developer.amd.com.cn/radeon/api/v1', {
    category: 'free', description: 'AMD’s free model API on its China site, with a daily cap.', website: 'https://developer.amd.com.cn/radeon/', keyUrl: 'https://developer.amd.com.cn/radeon/',
    quirks: { singleToolCall: true, timeoutMs: 600000 },
    billing: { defaultMode: 'free_tier', freeTierAll: true, note: 'Free trial service with a daily points cap (reset 00:00 Beijing time); not for production and there is no way to pay.', source: 'https://amd-aim.github.io/radeon-cloud-docs/guides/token-factory/' },
    limits: { pool: 'account', account: { rpm: 20, concurrent: 8 } },
  }),
  // Without a plan, Pro models have no credit to draw on and are refused, not charged.
  provider('agnes', 'Agnes AI', 'https://apihub.agnes-ai.com/v1', {
    category: 'free', description: 'Agnes’ own models; the Flash models are free during a promotion.', website: 'https://agnes-ai.com', keyUrl: 'https://platform.agnes-ai.com',
    billing: { defaultMode: 'free_tier', freeTierModels: ['-flash$'], note: 'Flash text, image and video models are free for people without a plan during a limited-time promotion. Pro models are charged from plan or pack credits.', source: 'https://wiki.agnes-ai.com/en/docs/pricing.md' },
    limits: { defaults: { rpm: 20 }, models: [{ match: 'video', rpm: 1 }] },
  }),
  provider('routeway', 'Routeway', 'https://api.routeway.ai/v1', {
    category: 'aggregator', description: 'About 280 models; models ending in :free cost nothing, 200 a day.', website: 'https://routeway.ai', keyUrl: 'https://routeway.ai/dashboard',
    quirks: { userAgent: BROWSER_UA },
    billing: { alwaysFree: [':free$'], note: 'Models ending in :free: 200 a day (reset 00:00 UTC), one at a time. Paid models need a positive balance.', source: 'https://docs.routeway.ai/getting-started/faq.md' },
    limits: { pool: 'account', poolMatch: ':free$', account: { rpm: 5, rpd: 200 } },
  }),
  provider('ainative', 'AINative Studio', 'https://api.ainative.studio/api/v1', {
    category: 'plan', description: 'Developer platform with a model API on monthly credit plans.', website: 'https://ainative.studio', keyUrl: 'https://ainative.studio/dashboard',
    billing: { note: 'Paid plans from 5 USD a month (3-day trial) with monthly credits.', source: 'https://ainative.studio/pricing' },
  }),
  provider('aion', 'Aion Labs', 'https://api.aionlabs.ai/v1', {
    category: 'free', description: 'Aion Labs’ roleplay and storytelling models with a small daily free allowance.', website: 'https://www.aionlabs.ai', keyUrl: 'https://www.aionlabs.ai/app/api-keys/',
    billing: { defaultMode: 'free_tier', freeTierAll: true, note: 'Free tier without a card: about 20,000 tokens a day. Once you top up, credits are spent; switch to paid then.', source: 'https://www.aionlabs.ai/docs/rate-limits/' },
    limits: { pool: 'account', account: { rpm: 15, tpm: 20000, tpd: 20000 } },
  }),

  // ----------------------------------------------------------------- gateways (checked 2026-11-07)
  provider('navy', 'NavyAI', 'https://api.navy/v1', {
    category: 'plan', description: 'One key for 130+ models on daily token plans; the free plan has 150K tokens a day.', website: 'https://api.navy',
    quirks: { userAgent: BROWSER_UA },
    // Models that name a required plan are never free (model_info reads required_plan).
    billing: { defaultMode: 'free_tier', note: 'Free plan: 150K tokens a day, 20 requests a minute; each model uses tokens at its own multiplier. Premium models need a paid plan (from 10 USD a month).', source: 'https://api.navy' },
    limits: { pool: 'account', account: { rpm: 20, tpd: 150000 } },
  }),
  provider('nara', 'NaraRouter', 'https://router.bynara.id/v1', {
    category: 'plan', description: 'Indonesian gateway with a small free plan (one model) and day or week token passes.', website: 'https://router.bynara.id', keyUrl: 'https://router.bynara.id',
    billing: { note: 'Free plan: one model, 7M tokens a day, 15 requests a minute, after Telegram verification or a 1 USD top-up. Everything else is prepaid credit or day/week passes.', source: 'https://router.bynara.id/#pricing' },
  }),
  provider('orcarouter', 'OrcaRouter', 'https://api.orcarouter.ai/v1', {
    category: 'aggregator', description: 'Zero-markup gateway; models ending in -free cost nothing within tight daily caps.', website: 'https://www.orcarouter.ai', keyUrl: 'https://www.orcarouter.ai/console',
    billing: { defaultMode: 'free_tier', freeTierModels: ['-free$', '^orcarouter/free$'], note: 'Models ending in -free never touch the wallet: about 10 requests a minute and 50 a day (more after 20 USD lifetime spend), with a small prompt cap; needs an established GitHub account linked. Others bill a prepaid wallet.', source: 'https://docs.orcarouter.ai/routing/free-models' },
    limits: { models: [{ match: '-free$', rpm: 10, rpd: 50 }] },
  }),
  provider('unorouter', 'UnoRouter', 'https://api.unorouter.com/v1', {
    category: 'aggregator', description: '300+ models on one key; models ending in :free cost nothing, about one request a minute each.', website: 'https://unorouter.com', keyUrl: 'https://unorouter.com/en/token',
    billing: { defaultMode: 'free_tier', freeTierModels: [':free$'], note: 'Models ending in :free never use your balance, about one request a minute each. Some need a one-time consent per provider at unorouter.com/consents and are refused if you objected to data use. All others bill a prepaid USD balance.', source: 'https://unorouter.com/en/docs/platform/errors-and-rate-limits' },
    limits: { models: [{ match: ':free$', rpm: 1 }] },
  }),
  // Free models are marked access_tier "free" in the listing (read by model_info).
  provider('xkiro', 'xKiro', 'https://api.xkiro.com/v1', {
    category: 'aggregator', description: '69 models on one key; about a third are free within a daily token allowance.', website: 'https://xkiro.com', keyUrl: 'https://xkiro.com/dashboard/api/keys',
    validate: { path: '/usage' },
    billing: { defaultMode: 'free_tier', note: 'Models marked free share 500K tokens a day (1M with Telegram verified). Paid models bill a prepaid wallet; images past the free allowance and web tools also charge it.', source: 'https://docs.xkiro.com/guides/pricing/' },
    limits: { pool: 'account', account: { tpd: 500000 } },
  }),
  // Pay as you go from a prepaid balance. Its zero-priced models only work inside
  // OpenCode itself ("Free usage limit reached. Free models work in OpenCode"), so
  // they are left out. /models is public, so the key is checked with a 1-token
  // request on a cheap model. Requests need a session header.
  provider('opencode', 'OpenCode Console', 'https://opencode.ai/inference/v1', {
    category: 'paid', description: 'Pay-as-you-go access to coding models tested by OpenCode (formerly Zen).', website: 'https://opencode.ai/docs/console/models/', keyUrl: 'https://opencode.ai/console',
    sessionHeader: 'x-opencode-session',
    quirks: { userAgent: 'NeoRouter' },
    models: { excludeFree: true },
    validate: { chatProbe: true, probeModels: ['glm-5.3-flash'] },
    billing: { note: 'Prepaid credits with a 4.6% + 0.31 USD fee per purchase; auto-recharge can charge your card. Its free models only work inside OpenCode, so NeoRouter leaves them out. OpenAI and Anthropic models keep requests 30 days; most others keep nothing.', source: 'https://opencode.ai/docs/console/models/' },
  }),
  // Free models are marked tier "free" in the listing.
  provider('airforce', 'Api.Airforce', 'https://api.airforce/v1', {
    category: 'aggregator', description: 'Cheap gateway to 790 models; the free plan is 1 request a minute on about 28 basic models.', website: 'https://api.airforce', keyUrl: 'https://api.airforce/dashboard/',
    billing: { defaultMode: 'free_tier', note: 'Free plan: models marked free, 1 request a minute, 1,000 a day. Topping up switches on pay as you go automatically; plans from 9.99 USD a month.', source: 'https://api.airforce/pricing/' },
    limits: { pool: 'account', account: { rpm: 1, rpd: 1000 } },
  }),
  provider('apinex', 'APInex', 'https://api.apinex.bond/v1', {
    category: 'aggregator', description: 'Crypto-funded gateway to 46 models; models starting with free/ use a daily free allowance.', website: 'https://apinex.bond', keyUrl: 'https://apinex.bond/keys',
    validate: { path: '/balance' },
    billing: { defaultMode: 'free_tier', freeTierModels: ['^free/'], note: 'Models starting with free/ never touch the wallet and use a daily allowance metered by weight. Others bill a prepaid USD wallet topped up with crypto.', source: 'https://apinex.bond/llms-full.txt' },
    limits: { pool: 'account', account: { rpm: 15 } },
  }),
  provider('blaze', 'BlazeAPI', 'https://api.blazeapi.org/paid/v1', {
    category: 'aggregator', description: 'Discounted gateway to 150+ models; 18 free/ models with 450K tokens a day.', website: 'https://blazeapi.org', keyUrl: 'https://blazeapi.org/dashboard',
    validate: { path: '/usage' },
    billing: { defaultMode: 'free_tier', freeTierModels: ['^free/'], note: 'Models starting with free/ are free: 450K tokens a day, 10 requests a minute, after email verification. Others bill a prepaid crypto balance (from 1 USD).', source: 'https://blazeapi.org/faq' },
    limits: { pool: 'account', poolMatch: '^free/', account: { rpm: 10, tpd: 450000 } },
  }),
  // Free models are marked billing_mode "free" in the listing.
  provider('blockrun', 'BlockRun', 'https://api.blockrun.ai/v1', {
    category: 'paid', description: 'Pay-per-call API for 117 models, by card or USDC; nine open models are free.', website: 'https://blockrun.ai', keyUrl: 'https://user.blockrun.ai',
    validate: { path: '/account' },
    billing: { note: 'Nine open models are free (30 requests a minute and 300 an hour per IP). Others bill prepaid credit, or a monthly invoice on enterprise accounts.', source: 'https://blockrun.ai/docs/api-reference/rate-limits' },
  }),
  provider('clod', 'CLōD', 'https://api.clod.io/v1', {
    category: 'aggregator', description: 'Low-cost inference for 50+ models; 100 free requests a day on 8 open models.', website: 'https://clod.io', keyUrl: 'https://app.clod.io',
    // Past the daily quota even "free" models bill a funded wallet, so nothing is marked free.
    billing: { note: '100 free requests a day on 8 open models (1,000 after a 25 USD purchase). Past that, even free models bill the wallet per token if it has money.', source: 'https://clod.io/pricing' },
    limits: { pool: 'account', account: { rpd: 100 } },
  }),
  provider('dreamprompting', 'DreamPrompting', 'https://dreamprompting.com/api/v1', {
    category: 'free', description: 'Free router over providers’ free tiers; every model is free within a daily quota.', website: 'https://dreamprompting.com', keyUrl: 'https://dreamprompting.com/account',
    billing: { defaultMode: 'free_tier', freeTierAll: true, note: 'Every model is free: 5,000 requests and 500K tokens a day, 100 a minute. Pro is a flat 9 USD a month with no per-use charges.', source: 'https://dreamprompting.com/pricing' },
    limits: { pool: 'account', account: { rpm: 100, rpd: 5000, tpd: 500000 } },
  }),
  provider('experiential', 'Experiential Labs', 'https://api.experientiallabs.ai/v1', {
    category: 'plan', description: 'One key for 959 models; the free plan covers three models with 1M tokens a day (card check needed).', website: 'https://www.experientiallabs.ai', keyUrl: 'https://platform.experientiallabs.ai/api-keys',
    // Only :free variants stop at the limit; others can continue on credits.
    billing: { alwaysFree: [':free$'], note: 'Free plan: GPT-6 Luna, DeepSeek V4.1 Flash and Clef share 1M tokens a day, 10 requests a minute, after a 1 USD card hold. With "Keep going on credits" on, use past that bills your balance; models ending in :free always stop instead.', source: 'https://platform.experientiallabs.ai/docs/billing' },
    limits: { pool: 'account', account: { rpm: 10, tpd: 1000000 } },
  }),
  provider('gizmo', 'Gizmo Platforms', 'https://api.gizmoplatforms.com/api/v1', {
    category: 'aggregator', description: 'Playground and API at provider cost plus a small fee; zero-cost models are free within a monthly quota.', website: 'https://gizmoplatforms.com', keyUrl: 'https://gizmoplatforms.com/developers',
    billing: { note: 'Free plan: 1 USD of usage a week and 1,000 free-model API calls a month, 20 a minute. Beyond that, provider cost plus 5% from a prepaid balance.', source: 'https://gizmoplatforms.com/developers' },
    limits: { pool: 'account', account: { rpm: 20 } },
  }),
  provider('inferbase', 'Inferbase', 'https://api.inferbase.ai/v1', {
    category: 'aggregator', description: 'AI gateway with its own hosted models; the free plan covers cheap models within daily caps.', website: 'https://inferbase.ai', keyUrl: 'https://inferbase.ai/register',
    validate: { path: 'https://api.inferbase.ai/api/v1/inference/credits' },
    // Free accounts hold no credit, so dearer models are refused, never charged.
    billing: { defaultMode: 'free_tier', freeTierAll: true, note: 'Free plan without a card: 5,000 requests a month; hosted models up to 2 USD per million output tokens are free within 100 requests and 0.50 USD a day. Dearer models need credit and are refused without it, not charged.', source: 'https://inferbase.ai/pricing' },
    limits: { pool: 'account', account: { rpm: 10, rpd: 100 } },
  }),
  // Free models are marked is_free in the listing. The free Qwen offer ends 2026-11-24.
  provider('iteracompute', 'IteraCompute', 'https://api.iteracompute.com/v1', {
    category: 'paid', description: 'Open models (Qwen, GLM, Kimi, DeepSeek) pay as you go, with one free Qwen model.', website: 'https://iteracompute.com', keyUrl: 'https://platform.iteracompute.com/api-keys',
    validate: false,
    billing: { note: 'Qwen3.8-27B is free (30M tokens a month, no card) until 24 November 2026; other models are charged from your balance.', source: 'https://iteracompute.com/pricing' },
    limits: { models: [{ match: '^qwen/qwen3\\.8-27b$', tpm: 160000 }] },
  }),
  // Free models are marked tier "free" in the listing.
  provider('kiosapi', 'KiosAPI', 'https://api.kiosapi.id/v1', {
    category: 'aggregator', description: 'Indonesian AI gateway priced in rupiah, with five free models.', website: 'https://kiosapi.id', keyUrl: 'https://kiosapi.id/dashboard/keys',
    billing: { defaultMode: 'free_tier', note: 'Five models are free: 50 requests a day, 5 a minute, after email verification. Other models are charged in rupiah from a prepaid balance.', source: 'https://kiosapi.id/docs' },
    limits: { models: [{ match: '^(meta/llama-3\\.1-8b|qwen/qwen3-30b|zai/glm-4\\.7-flash|google/gemma-4-26b|aisingapore/sea-lion-27b)$', rpm: 5, rpd: 50 }] },
  }),
  // Probing this provider's key endpoints gets accounts suspended.
  provider('llmtr', 'LLMTR', 'https://llmtr.com/v1', {
    category: 'aggregator', description: 'Turkish AI gateway with Turkey-hosted and global models, prepaid credit.', website: 'https://llmtr.com',
    validate: false,
    billing: { alwaysFree: ['-free$'], note: 'Prepaid credit (top-ups carry an 8% margin). Models ending in -free cost nothing; their limits are not published.', source: 'https://llmtr.com/pricing' },
  }),
  provider('logfare', 'Logfare', 'https://logfare.ai/v1', {
    category: 'free', description: 'Free inference with fair-use limits; prompts are logged.', website: 'https://logfare.ai', keyUrl: 'https://logfare.ai/register',
    billing: { defaultMode: 'free_tier', freeTierAll: true, note: 'Free, no card, no paid tier. All traffic is logged and may be used for evaluation; premium models need a training opt-in. New accounts have lower limits for their first day.', source: 'https://logfare.ai/docs' },
    limits: { pool: 'account', account: { rpm: 40, rpd: 5000, concurrent: 6 } },
  }),
  provider('lucidity', 'Lucidity Composite', 'https://composite.lucidity.sh/v1', {
    category: 'aggregator', description: 'Roleplay-oriented API proxy with a small daily free allowance.', website: 'https://composite.lucidity.sh',
    billing: { defaultMode: 'free_tier', freeTierModels: [':free$', '^lucidityai/synth-2\\.5-flash$'], note: 'Models ending in :free share 80 requests a day; Synth has its own 80. Some free "contributor" models train on prompts. Others need credits (provider price + 25%) or a plan.', source: 'https://composite.lucidity.sh' },
    limits: { pool: 'account', poolMatch: ':free$', account: { rpd: 80 } },
  }),
  provider('onomeo', 'Onomeo', 'https://onomeo.com/v1', {
    category: 'paid', description: 'Pay-as-you-go API for major models (beta).', website: 'https://onomeo.com', keyUrl: 'https://onomeo.com/dashboard',
    billing: { note: 'Prepaid balance (from 5 USD). Free answers apply only to its web chat, not the API.', source: 'https://onomeo.com/models' },
    limits: { defaults: { rpm: 12 } },
  }),
  provider('plugsky', 'PlugSky', 'https://plugsky.com/v1', {
    category: 'plan', description: 'Its own routed models; two free models, the rest on monthly plans.', website: 'https://plugsky.com', keyUrl: 'https://plugsky.com/dashboard',
    validate: { path: 'https://plugsky.com/v1/plugsky/usage' },
    billing: { defaultMode: 'free_tier', freeTierModels: ['^plugsky-(micro|lite)$'], note: 'Free plan without a card: plugsky-micro and plugsky-lite, 500 requests and 250K tokens a day (less for new accounts). Others need a plan (8 to 120 USD a month).', source: 'https://plugsky.com/pricing' },
    limits: { pool: 'account', account: { rpm: 30, rpd: 500, tpd: 250000 } },
  }),
  provider('router9', 'Router9', 'https://api.router9.com/v1', {
    category: 'plan', description: 'Image, video and chat models on a monthly credit plan.', website: 'https://router9.com', keyUrl: 'https://router9.com/connect/keys',
    billing: { defaultMode: 'plan', plan: { note: 'Monthly credits: Free (about 1 USD), Pro 29 USD, Max 59 USD.' }, note: 'Which models the free plan covers is not published.', source: 'https://router9.com/pricing' },
  }),
  provider('septor', 'Septor Labs', 'https://api.septorlabs.com/v1', {
    category: 'aggregator', description: 'Gateway to open and frontier models, with a 1 USD trial and capped free models.', website: 'https://septorlabs.com',
    billing: { note: 'A one-time 1 USD trial credit (90 days) and free models capped at 50 requests and 500K tokens a day; free traffic goes through third-party aggregators. Plans from 20 USD a month.', source: 'https://septorlabs.com/pricing' },
  }),
  provider('simplellm', 'SimpleLLM', 'https://api.simplellm.eu/v1', {
    category: 'paid', description: 'EU-hosted open models, pay per token.', website: 'https://simplellm.eu',
    validate: { path: '/rate-limit' },
    billing: { note: 'Its terms could not be verified on the official site.' },
  }),
  provider('speka', 'Speka', 'https://speka.me/v1', {
    category: 'aggregator', description: 'Gateway to open models with 1 USD of free usage a month.', website: 'https://speka.me', keyUrl: 'https://speka.me/dashboard/keys',
    billing: { defaultMode: 'free_tier', freeTierAll: true, note: 'Free plan without a card: 1 USD of model usage a month, 10 requests a minute; requests are refused once it is spent. Paid plans (19 to 399 USD) bill overage.', source: 'https://speka.me/pricing' },
    limits: { pool: 'account', account: { rpm: 10 } },
  }),
  provider('waterfall', 'Waterfall', 'https://api.getwaterfall.org/v1', {
    category: 'aggregator', description: 'Gateway that routes to free models first, then paid ones.', website: 'https://getwaterfall.org', keyUrl: 'https://getwaterfall.org/dashboard/',
    billing: { defaultMode: 'free_tier', freeTierModels: ['-free$'], note: 'Free without a card on models ending in -free (community rate limits). Paid models need card credits (from 10 USD, 4% markup) or x402; its auto route may use them when credit exists.', source: 'https://getwaterfall.org/pricing/' },
  }),
];
