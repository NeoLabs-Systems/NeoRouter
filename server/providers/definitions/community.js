'use strict';

// Smaller OpenAI-compatible hosts. They default to "paid" because their free
// offers change often: switch an account to "free" in the UI only after
// confirming that account cannot be charged.
const BROWSER_UA = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36';

function community(id, label, baseUrl, extra = {}) {
  const { billing = {}, ...rest } = extra;
  return {
    id,
    label,
    description: rest.description || `${label} OpenAI-compatible endpoint.`,
    category: 'community',
    adapter: 'openai_compatible',
    baseUrl,
    ...rest,
    billing: { defaultMode: 'paid', ...billing },
  };
}

module.exports = [
  community('sealion', 'SEA-LION', 'https://api.sea-lion.ai/v1', { website: 'https://sea-lion.ai', billing: { defaultMode: 'free_tier', freeTierAll: true, note: 'Trial API key, rate-limited, no billing.' } }),
  community('typhoon', 'Typhoon', 'https://api.opentyphoon.ai/v1', { website: 'https://opentyphoon.ai', billing: { defaultMode: 'free_tier', freeTierAll: true, note: 'Free for light usage on this endpoint.' } }),
  community('longcat', 'LongCat', 'https://api.longcat.chat/openai/v1', { website: 'https://longcat.chat', billing: { defaultMode: 'free_tier', freeTierAll: true, note: 'Prepaid only: free while you never top up the balance (requests fail at $0).' } }),
  community('electronhub', 'Electron Hub', 'https://api.electronhub.ai/v1', { validate: { path: '/user/me' }, billing: { alwaysFree: [':free$'] } }),
  community('kenari', 'Kenari', 'https://kenari.id/v1', { validate: { path: '/account/balance' }, billing: { alwaysFree: [':free$'] } }),
  community('bazaarlink', 'BazaarLink', 'https://bazaarlink.ai/api/v1', { billing: { alwaysFree: [':free$'] } }),
  community('reka', 'Reka', 'https://api.reka.ai/v1', { website: 'https://reka.ai' }),
  community('qianfan', 'Baidu Qianfan', 'https://qianfan.baidubce.com/v2', { website: 'https://qianfan.cloud.baidu.com' }),
  community('volcengine', 'Volcengine Ark', 'https://ark.cn-beijing.volces.com/api/v3', { website: 'https://www.volcengine.com' }),
  community('xfyun', 'iFlytek Spark', 'https://spark-api-open.xf-yun.com/v1', { website: 'https://xinghuo.xfyun.cn' }),
  community('bai', 'B.AI', 'https://api.b.ai/v1'),
  community('anyapi', 'AnyAPI', 'https://api.anyapi.ai/v1', { website: 'https://anyapi.ai', billing: { defaultMode: 'free_tier', freeTierModels: [':free$'], note: 'Free plan without a card: the models ending in :free, about 100,000 tokens a day. Other models need credits.', source: 'https://anyapi.ai/pricing', checkedAt: '2026-10-29' } }),
  community('radeon', 'AMD Radeon Cloud', 'https://developer.amd.com.cn/radeon/api/v1', { quirks: { singleToolCall: true, timeoutMs: 600000 } }),
  community('agnes', 'Agnes AI', 'https://apihub.agnes-ai.com/v1'),
  community('routeway', 'Routeway', 'https://api.routeway.ai/v1', { quirks: { userAgent: BROWSER_UA } }),
  community('ainative', 'AINative Studio', 'https://api.ainative.studio/api/v1'),
  community('aion', 'Aion Labs', 'https://api.aionlabs.ai/v1'),
  community('navy', 'api.navy', 'https://api.navy/v1', { quirks: { userAgent: BROWSER_UA } }),
  community('nara', 'Nara Router', 'https://router.bynara.id/v1'),
  community('orcarouter', 'OrcaRouter', 'https://api.orcarouter.ai/v1'),
  community('unorouter', 'UnoRouter', 'https://api.unorouter.com/v1'),
  community('xkiro', 'xKiro', 'https://api.xkiro.com/v1', { validate: { path: '/usage' } }),
  community('opencode', 'OpenCode Zen', 'https://opencode.ai/zen/v1', { headers: { 'x-opencode-session': 'neorouter' } }),
  community('airforce', 'api.airforce', 'https://api.airforce/v1'),
  community('apinex', 'Apinex', 'https://api.apinex.bond/v1', { validate: { path: '/balance' }, limits: { pool: 'account', account: { rpm: 5 } } }),
  community('blaze', 'BlazeAPI', 'https://api.blazeapi.org/paid/v1', { validate: { path: '/usage' } }),
  community('blockrun', 'BlockRun', 'https://api.blockrun.ai/v1', { validate: { path: '/account' } }),
  community('clod', 'CLōD', 'https://api.clod.io/v1'),
  community('dreamprompting', 'DreamPrompting', 'https://dreamprompting.com/api/v1'),
  community('experiential', 'Experiential Labs', 'https://api.experientiallabs.ai/v1'),
  community('gizmo', 'Gizmo Platforms', 'https://api.gizmoplatforms.com/api/v1'),
  community('inferbase', 'Inferbase', 'https://api.inferbase.ai/v1', { validate: { path: 'https://api.inferbase.ai/api/v1/inference/credits' } }),
  community('iteracompute', 'IteraCompute', 'https://api.iteracompute.com/v1', { validate: false }),
  community('kiosapi', 'KiosAPI', 'https://api.kiosapi.id/v1'),
  // Probing this provider's key endpoints gets accounts suspended.
  community('llmtr', 'LLMTR', 'https://llmtr.com/v1', { validate: false }),
  community('logfare', 'Logfare', 'https://logfare.ai/v1'),
  community('lucidity', 'Lucidity', 'https://composite.lucidity.sh/v1'),
  community('onomeo', 'Onomeo', 'https://onomeo.com/v1'),
  community('plugsky', 'PlugSky', 'https://plugsky.com/v1', { validate: { path: 'https://plugsky.com/v1/plugsky/usage' } }),
  community('router9', 'Router9', 'https://api.router9.com/v1'),
  community('septor', 'Septor Labs', 'https://api.septorlabs.com/v1'),
  community('simplellm', 'SimpleLLM', 'https://api.simplellm.eu/v1', { validate: { path: '/rate-limit' } }),
  community('speka', 'Speka', 'https://speka.me/v1'),
  community('waterfall', 'Waterfall', 'https://api.getwaterfall.org/v1'),
];
