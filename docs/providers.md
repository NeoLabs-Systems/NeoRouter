# Providers

Provider definitions are **data**. They live in
`internal/providers/definitions.json`, which is embedded in the binary and
validated at startup (`internal/providers/load.go`). Models that run on the
server itself (Admin → Local models) have their own catalog in
`internal/localmodels/catalog.go`.

## Adding an OpenAI-compatible provider

Every entry in `definitions.json` writes out all its fields: there are no
defaults. Copy an existing `openai_compatible` entry and change it. The registry
tests also record each provider's connect-screen view in
`internal/providers/testdata/behaviour_fixtures.json` (`publicDefinition`), so
add one there too. These are the fields you normally change:

```json
{
  "id": "example",
  "label": "Example AI",
  "description": "One line shown in the provider gallery.",
  "category": "free",
  "website": "https://example.ai",
  "keyUrl": "https://example.ai/keys",
  "adapter": "openai_compatible",
  "baseUrl": "https://api.example.ai/v1",
  "billing": {
    "defaultMode": "free_tier",
    "freeTierAll": true,
    "freeTierModels": ["^small-"],
    "alwaysFree": [":free$"],
    "verify": { "type": "ratelimit_headers", "maxFreeRequestsPerDay": 14400 },
    "note": "Shown under the billing selector.",
    "source": "https://example.ai/pricing",
    "checkedAt": "2026-10-28"
  },
  "limits": {
    "pool": "model",
    "account": { "rpm": 60 },
    "defaults": { "rpm": 30, "rpd": 1000 },
    "models": [{ "match": "70b", "rpm": 10 }]
  }
}
```

- `id` is stable and used for the default slug; `category` is `free`, `paid`,
  `aggregator`, `local`, `custom` or `community`; `keyUrl` is the "Get a key"
  button.
- `freeTierAll` means the free tier covers every model, `freeTierModels` limits
  it to those patterns, `alwaysFree` models are free whatever the billing mode.
  `source` and `checkedAt` record where the facts come from.
- `limits` are the documented free-tier limits (response headers override them);
  `pool` is `model` or `account` (limits shared by all models).

Optional fields: `auth` (`bearer`, `header` + `header`, `none`, `anonymousKey`),
`keyOptional`, `fields` (extra account inputs templated into `baseUrl` as `{key}`),
`headers`, `chatPath`, `models.path` / `models.static` / `models.facts`, `validate` (`{ path }`,
`{ chatProbe: true }` or `false`), `quota.headerPeriods`, `quota.probe`
(`{ path, parse }`, where `parse` names a parser in
`internal/providers/quota.go`) and `quirks`
(`timeoutMs`, `userAgent`, `singleToolCall`, `noTools`, `maxTokensField`,
`minMaxTokens`, `dropFields`, `stripSchemaKeys`, `streamUsage`).

`models.facts` records documented per-model limits (`id → { contextWindow, maxOutput }`)
for hosts whose listing omits them — Kourier, for example, lists only ids. A sync
fills in what the listing leaves empty: values the listing itself reports always
win, and the definition's facts beat models.dev for that provider. Name the source
and check date in the commit, as with billing facts. Without either source the
catalog falls back to canonical metadata, which can overstate what a specific
host serves.

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
attached must declare a check (`internal/providers/billing.go`).
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

## Signing in instead of pasting a key

Some providers are connected with their own sign-in, which returns an OAuth
token instead of an API key. Set `connect`:

- `codex_device` — ChatGPT (Codex) plans, `internal/providers/codex_oauth.go`.
- `nous_device` — Nous Portal subscriptions and credits,
  `internal/providers/nous_oauth.go`.

Both use a device code (the user approves a code in the browser, so it works on
a server without one) and store the tokens as the account secret, refreshed
before they expire. Routes live in `internal/providers/routes.go`
(`/providers/chatgpt/login`, `/providers/nous/login`); a definition with `connect`
must have an adapter that knows how to turn the stored secret into an auth
header (`adapters/codex.go`, `adapters/nous.go`).

A `connect` provider may also accept a pasted key: the Nous adapter sends an
API key as it is and only treats a three-part JWT as sign-in tokens.

## Non-OpenAI protocols

Adapters live in `internal/providers/adapters/` and implement the interface in
`internal/providers/adapter.go`; chat is always in the canonical (OpenAI Chat)
format. A new dialect gets a name in `internal/providers/names.go` and a case in
`adapters/chat_factory.go` (or `media_factory.go`), built on `neolabs-go/llm`
where that covers the protocol.
