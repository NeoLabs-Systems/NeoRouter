# Providers

Provider definitions are **data**. They live in `server/providers/definitions/`
and are loaded and validated (zod) at startup by `server/providers/registry.js`.
A file may export one definition or an array.

## Adding an OpenAI-compatible provider

```js
'use strict';

module.exports = {
  id: 'example',                       // stable id, used for the default slug
  label: 'Example AI',
  description: 'One line shown in the provider gallery.',
  category: 'free',                    // free | paid | aggregator | local | custom | community
  website: 'https://example.ai',
  keyUrl: 'https://example.ai/keys',   // "Get a key" button
  adapter: 'openai_compatible',
  baseUrl: 'https://api.example.ai/v1',
  billing: {
    defaultMode: 'free_tier',          // free_tier | paid
    freeTierAll: true,                 // a free-tier account cannot be charged at all
    alwaysFree: [':free$'],            // regexes: models free regardless of billing mode
    note: 'Shown under the billing selector.',
  },
  limits: {                            // documented free-tier limits (headers override them)
    pool: 'model',                     // model | account (limits shared by all models)
    account: { rpm: 60 },
    defaults: { rpm: 30, rpd: 1000 },
    models: [{ match: '70b', rpm: 10 }],
  },
};
```

Optional fields: `auth` (`bearer`, `header` + `header`, `none`, `anonymousKey`),
`keyOptional`, `fields` (extra account inputs templated into `baseUrl` as `{key}`),
`headers`, `chatPath`, `models.path` / `models.static`, `validate` (`{ path }`,
`{ chatProbe: true }` or `false`), `quota.headerPeriods`, `quota.probe`
(`{ path, parse(data) → [{ metric, limit, remaining }] }`) and `quirks`
(`timeoutMs`, `userAgent`, `singleToolCall`, `noTools`, `maxTokensField`,
`minMaxTokens`, `dropFields`, `stripSchemaKeys`, `streamUsage`).

## Free classification (strict)

A model is free only if one of these holds — an unknown price is never free:

1. the user marked it free (model override),
2. it matches `billing.alwaysFree`,
3. the provider listing reports every price field as exactly 0 (and `trustListingFree` is not false),
4. the account's billing mode is `free_tier` and `billing.freeTierAll` is true.

## Non-OpenAI protocols

Adapters live in `server/providers/adapters/` and implement `validate`,
`listModels`, `chat`, `stream` and optionally `fetchQuota`, always in the
canonical (OpenAI Chat) format. Existing: `openai_compatible`, `anthropic`,
`google_gemini`, `cloudflare`. Register a new adapter in `ADAPTERS` in
`registry.js`.
