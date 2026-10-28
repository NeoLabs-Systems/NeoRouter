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
    freeTierAll: true,                 // the free tier covers every model …
    freeTierModels: ['^small-'],       // … or only these (regexes)
    alwaysFree: [':free$'],            // regexes: models free regardless of billing mode
    verify: { type: 'ratelimit_headers', maxFreeRequestsPerDay: 14400 }, // see below
    note: 'Shown under the billing selector.',
    source: 'https://example.ai/pricing', checkedAt: '2026-10-28',  // where the facts come from
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
3. the provider listing reports every price field as exactly 0 (negative or
   dynamic prices and `paid_only` / `usage_based_only` flags count as paid),
4. the account's billing mode is `free_tier`, the model is covered by
   `freeTierAll` / `freeTierModels`, and — for providers with `billing.verify`
   — an automatic check proved the account cannot be charged.
   `localOnly` limits this to loopback/private base URLs, `keylessOnly` to
   accounts without a key.

Evidence from real traffic beats rules 2–4 (only a manual override beats it):

- a provider that reports a charge (`usage.cost`) for a "free" model marks it
  paid for good (`billed_observed`);
- a "free_tier … limit: 0" refusal marks the model as having no free quota on
  that account (`no_free_tier`).

### Billing verification (`billing.verify`)

Providers whose free tier silently ends once a card or billing account is
attached must declare a check (`server/services/providers/billing_check.js`).
It runs when the account is added, when its billing mode or key changes, and
every 6 hours. Until it returns "free", rule 4 does not apply.

- `ratelimit_headers` (Groq): the daily request limit in the response headers
  is above `maxFreeRequestsPerDay` only on paid plans. Also checked passively on
  every routed response.
- `paid_model_probe` (Google): a one-token request to a model without a free
  tier. A project without billing is refused with a free-tier limit of 0 (not
  billed); a billed project answers.

Only use `freeTierAll` when the provider cannot charge at all on that plan, and
record `source` and `checkedAt` whenever free-tier facts change.

## Non-OpenAI protocols

Adapters live in `server/providers/adapters/` and implement `validate`,
`listModels`, `chat`, `stream` and optionally `fetchQuota`, always in the
canonical (OpenAI Chat) format. Existing: `openai_compatible`, `anthropic`,
`google_gemini`, `cloudflare`. Register a new adapter in `ADAPTERS` in
`registry.js`.
