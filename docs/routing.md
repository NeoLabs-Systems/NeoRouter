# Routing

## Model names

- `provider-slug/model-id` — that exact model first; the same model on your
  other providers (matched by canonical id) is the fallback.
- `model-id` or a canonical name — every provider that serves it.
- `auto` — the key's routing profile. `auto:<profile>` picks a preset
  (`reliable`, `fast`, `balanced`, `best`, `smart`, `cheap`) or a custom profile id.

## Pipeline (`server/services/routing/router.js`)

1. **Request profile** — estimated input tokens, needed capabilities (tools,
   vision, JSON), complexity (length, code, tools).
2. **Hard filters** — enabled/available, key policy (free-only, allowed model
   globs, budget incl. worst-case cost, unknown price excluded for budget keys),
   capabilities, context window, profile constraints, cooldown / circuit
   breaker, quota windows.
3. **Features** in [0, 1]: intelligence (metadata), speed (measured TTFT and
   tokens/s, prior from metadata), reliability (Thompson sample from a decayed
   Beta posterior), cost (log price, free = 1).
4. **Score** = weighted features × quota headroom × penalty factor × load factor.
   Adaptive profiles shift weight to intelligence for complex requests.
5. **Selection** — softmax draw among the top-k (exploration temperature),
   so bursts spread out; in-flight leases lower scores immediately.

## Learning (`learning.js`, `stats_store.js`)

| Error kind | Effect |
|---|---|
| rate_limit (429) | penalty +2, cooldown max(Retry-After, 90 s), limit learned from message |
| quota_exhausted | cooldown ladder 2 min → 10 min → 1 h → 24 h |
| payment (402) | cooldown 24 h |
| auth (401/403) | cooldown 30 min, background key re-check, account disabled after 3 confirmed rejections |
| not_found | cooldown 6 h |
| context_length | no penalty, context window learned |
| server / timeout / network / empty / invalid tool JSON | failure + penalty, counts for circuit breaker (3 in 15 min → 10 min pause, then half-open probe) |
| slow (> 2.5 × own EWMA) | soft failure, small penalty |

Outcome counts decay with a 24 h half-life, penalties with a 5 min half-life.

Streams are committed to the client only after the first real output arrives,
so silent or failing providers are replaced transparently.

## Intelligence

`server/services/catalog/metadata_service.js` combines models.dev and
OpenRouter (prices, context, capabilities, release dates) and, if an admin
configured a key, Artificial Analysis (benchmark index, measured speed).
Without a benchmark the score is a heuristic of market price, parameter count,
recency and reasoning support. Users can override it per model.
