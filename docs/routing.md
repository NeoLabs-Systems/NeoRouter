# Routing

## Model names

- `provider-slug/model-id` — that exact model first; the same model on your
  other providers (matched by canonical id) is the fallback.
- `model-id` or a canonical name — every provider that serves it.
- `auto` — the key's routing profile. `auto:<profile>` picks a preset
  (`reliable`, `fast`, `balanced`, `best`, `smart`, `cheap`) or a custom profile id.

## Routing groups

Create a named group on **Routing → Routing groups**. Select accounts (all their
models) and/or model patterns (matched against `provider-slug/model-id`). Members
are combined as a union. A group can set a routing style, minimum intelligence and
a fallback group. Names are stable routing identifiers; fallback cycles are rejected.
These groups are independent of the shared-capacity pools between users.

Select a group with `pool:<group-name>` as the model, `X-NeoRouter-Group` as a
request header, or the key's default routing group. The model name takes precedence
over the header, which takes precedence over the key default. A key default applies
to automatic routing; explicitly named models keep their normal behavior. Key
allowed-model patterns can include `pool:*` or individual group routes. A route
permission controls client selection; configured fallbacks still obey the key's
underlying model, capability, cost and privacy policies.

Groups reuse scoring, measured health, plan pacing, concurrency limits and the
capacity queue. If one group has no candidate, its fallback is tried. Queueing
watches every configured group and reconsiders the preferred group when capacity
opens. If the chain cannot serve, the existing last-resort pass can use models
outside the group; key restrictions and hard provider limits still apply.

Management API: `GET/POST /api/v1/routing/groups`, and
`PUT/DELETE /api/v1/routing/groups/:id`. The writable fields are `name`, `accounts`,
`models`, `profile`, `minIntelligence` and `fallback`. At least one account or model
pattern is required. The list includes current plan usage per member account;
unknown usage is shown as unknown. Activity records and `X-NeoRouter-Group` identify
the group that actually served a request, including a fallback.

## Flat-rate concurrency plans

Choose **Flat rate / concurrency slots** for a connection with a paid subscription
that charges nothing per request. This is a plan, not a free tier: keys must allow
plans, and request cost is zero. Kourier is a known provider using its authenticated
`/v1/models` listing. Starter/Pro slot counts come from its provider definition;
select the subscribed plan and override the concurrent limit for account bonuses.
Custom OpenAI/Anthropic endpoints can use the same billing kind and stated limits.
Model routing never relies on a fixed Kourier model name.

With flat-rate capacity in automatic routing, source preference within each style's
kind tier is: available flat-rate slot, allowance ahead of its renewal pace, other
prepaid allowance, free tier, paid usage. Health, capabilities, model permissions
and other hard filters still apply. Explicit model requests keep their existing
model-first behavior. Allowance pacing and concurrency remain learned from provider
responses; a stated concurrency limit continues to override learned/curated limits.

Each flat-rate account has an overflow policy:

- `auto`: overflow when a comparable genuinely free model can serve; otherwise wait.
- `overflow`: try another available source immediately when all slots are busy.
- `queue`: wait for a slot first, then overflow when the maximum wait expires.

The wait starts when routing starts and is bounded by `slotWaitMs` and the overall
request deadline. Its default is `NEOROUTER_SLOT_QUEUE_WAIT_MS`. Configure
`billingMode: "flat_rate"`, `concurrent`, `overflowPolicy` and `slotWaitMs` through
the provider account API or connection form. Kourier's plan selection is
`options.slotPlan`; concurrent overrides cover bonuses without changing a definition.
The Limits dialog can change both the concurrency limit and overflow policy.

The Limits page shows current, time-weighted average and peak busy slots, idle
capacity and a minute-by-minute usage chart for the last 24 hours. These are
router-observed counts, persisted as time integrals; downtime is excluded from
observed time. Unknown slot capacity yields unknown idle share. Allowance plans
also show pace relative to renewal (above 1 means ahead of schedule). Extra keys
from the same subscription should not be added as independent accounts because
the upstream shares their capacity.

## Provider cache awareness

Chat routing remembers the last successful account and model for a conversation,
scoped to the router key. It uses the existing client session headers or derives a
hash from the leading system/user messages. Only a hashed identifier and model
reference are retained in bounded, expiring process memory. Restarting the router
clears affinity; observed cache usage remains in the request history.

Affinity is a soft score benefit, scaled by the recent measured share of cached
input, request size and known cache discount. A model with no reported cache hits
receives no benefit. Normal health, capacity, quality and budget decisions can move
the conversation elsewhere. Configure expiry, memory size and learning window with
`NEOROUTER_CACHE_AFFINITY_TTL_MS`, `NEOROUTER_CACHE_AFFINITY_MAX_SESSIONS`,
`NEOROUTER_CACHE_LEARNING_WINDOW_MS` and `NEOROUTER_CACHE_AFFINITY_TOKEN_SCALE`.

OpenAI Chat/Responses requests preserve `prompt_cache_key`; absent a client value,
the conversation identifier supplies it. Anthropic translation preserves system
and tool cache breakpoints and adds ephemeral prefix breakpoints when absent.
JSON and streamed usage preserve cache reads and cache creation separately.
Provider listing prices and provider-specific models.dev facts supply cache rates;
`priceCachedIn` and `priceCacheWrite` model overrides use USD per million tokens.
Unknown cache prices fall back to the ordinary input price. Budget reservations
include known cache write premiums. Activity shows cached tokens and the percentage
of successful requests with cache hits for each provider.

This uses upstream prompt caches. NeoRouter never stores prompts or responses in
a local response cache.

## Pipeline (`internal/routing/router.go`)

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

## Spreading metered allowances until reset

When enabled in Settings → Usage, pacing combines observed remaining balances
with a recency-weighted hour-of-week demand profile. Requests and tokens learn
separate traffic shapes, and token forecasts include learned output sizes and
in-flight output reservations. Failed attempts do not teach a false demand peak.

Daily limits with an upstream reset use cumulative consumption since that reset;
yesterday's buckets do not block the renewed window. Unknown daily reset times
use a rolling window, its remaining balance and upcoming bucket expirations.
Provider observations retain their local-consumption baseline so sends between
observations are deducted, including concurrent sends within one minute.

Percentage plan windows learn units consumed per request from balance changes and
reserve that measured consumption between updates. The router shifts work toward
allowances with room before they exceed pace, then queues paced work until its
calculated release time. Last-resort routing preserves enabled pacing. If no
eligible source can serve within the request's queue budget, the response includes
a retry time; a sustained demand above total allowance cannot be solved by pacing.
Initial enablement requires sufficient history; adding unknown models does not
silently switch protection off for known allowances. High-priority keys bypass
pacing, while hard limits and key budgets still apply.
Flat-rate slots and pay-per-use sources are not metered allowances.

## Learning (`learning.go`, `stats.go`)

| Error kind | Effect |
|---|---|
| rate_limit (429) | penalty +2, cooldown max(Retry-After, 90 s), limit learned from message; with no answer in the last hour the cooldown climbs 90 s → 5 min → 30 min → 2 h → 6 h |
| quota_exhausted | cooldown ladder 2 min → 10 min → 1 h → 24 h |
| payment (402) | cooldown 24 h |
| auth (401/403) | cooldown 30 min, background key re-check, account disabled after 3 confirmed rejections |
| not_found, or 401/403 about the model or plan (not on this plan, consent, phone verification) | cooldown 6 h → 24 h → 7 d |
| context_length | no penalty, context window learned |
| server / timeout / network / empty / invalid tool JSON | failure + penalty, counts for circuit breaker (3 in 15 min → 10 min pause, then half-open probe) |
| slow (> 2.5 × own EWMA) | soft failure, small penalty |

Outcome counts decay with a 24 h half-life, penalties with a 5 min half-life.

One connection may cost a single request two failed attempts, then the others
get a turn. After 30 failures in a row without a success, a whole connection
rests for 5 minutes, doubling up to an hour (`health.go`). While at least
30 % of a user's recent requests go unanswered (`capacity.go`), requests wait
5 seconds instead of 45 for a model to free up (high-priority keys still wait in
full) and at most one hedge runs per request. A client sending the same conversation again within
3 minutes of an answer counts as a bad rating of that answer; only a hash of the
conversation is kept, in memory.

Benchmark scores with a confidence below 0.5 are pulled towards 0.25, so a model
nobody measured cannot outrank measured ones, and a model far below what the task
needs (under 0.08 to 0.12) keeps 60 % of its score.

Streams are committed to the client only after the first real output arrives,
so silent or failing providers are replaced transparently.

## Intelligence

`internal/catalog/metadata_refresh.go` combines models.dev and
OpenRouter (prices, context, capabilities, release dates), OpenRouter's daily
benchmark leaderboard (GPQA Diamond and tau2-bench airline, the newest 90 days;
CC BY 4.0) and, if an admin configured a key, Artificial Analysis (benchmark
index, measured speed). Where both benchmark sources know a model, its overall
score is 60 % Artificial Analysis and 40 % OpenRouter. An OpenRouter benchmark
that ranks models very differently from the fuller index gets no say.

Artificial Analysis refreshes every 6 hours: a Free key allows 100 requests a day.
Free lists carry the index and speed but no per-benchmark scores, so the scores
already known are kept (a Pro key keeps them current); models without an index
get one estimated from them.
Without a benchmark the score is a heuristic of market price, parameter count,
recency and reasoning support. Users can override it per model.

## Other kinds

Embeddings, images, speech, transcription, System One (moderation), video
and web search use the same plan, learning, quotas and budgets
(`internal/gateway/run_capability.go`); only the adapter method and
the price unit differ. Endpoints follow the OpenAI API: `/v1/embeddings`,
`/v1/images/generations`, `/v1/audio/speech`, `/v1/audio/transcriptions`
(and `/translations`), `/v1/moderations`, `/v1/videos` and `/v1/search`. `GET
/v1/models?type=embedding` lists a kind.

- **Embeddings:** "auto" pins a key to the first model and size it used, since
  vectors of different models cannot be compared; fallback then only moves
  between hosts of that model. `DELETE /v1/embeddings/pin` resets it.
- **Transcription:** subtitles (`srt`, `vtt`, `verbose_json`) prefer models
  that return timestamps.
- **System One:** guard models answer in their own formats (a probability,
  `unsafe\nS9`, JSON verdicts); `internal/media/systemone.go` maps
  them to OpenAI moderation results. `check` picks `safety` (default),
  `prompt_injection` or `topic` (with a `policy`).
- **Video:** jobs stay bound to the provider that accepted them and are polled
  on `GET /v1/videos/:id`.
- **Web search:** each search engine is one `search` model
  (`internal/providers/definitions.json`). Chat requests that ask for
  built-in search (`web_search_options`, OpenRouter's `plugins: [{ id: 'web' }]`
  or a `:online` model, a Responses `web_search` tool, an Anthropic
  `web_search_*` tool) get a `web_search` function; NeoRouter runs its calls
  through the search engines (up to 5 per request, `max_uses` up to 20, domain
  filters honoured) and loops until the model answers
  (`internal/gateway/websearch.go`). Without a search engine the
  request runs as before. Queries pass the privacy shield again first.
- **Quality:** image, speech and video models are scored by Artificial
  Analysis arena ELO; other kinds by size and age within their kind.
- **Prices:** per image, per second of video, per minute of audio or per
  million characters (`price_unit`), or per token where the provider bills
  that way.

## Several accounts of one provider

Each account's copy of a model is its own candidate with its own stats,
cooldowns and quota pool. Copies of the same model compete only on health,
allowance left and free capacity (`spreadAcrossAccounts` in `rank.go`), so
accounts wear evenly without changing which model wins. Concurrency limits are
learned (`concurrency.go`): a 429 while several requests run on an account sets
its limit below that count, steady success at the limit probes one higher, and
learned limits fade after an hour. An account at its limit is skipped
(`account_busy`) and chat requests wait briefly if every copy is busy.

## Key priority

Each key or connected app is `high`, `normal` (default) or `low`. Priority only
counts when capacity runs short: a request that has to wait for a model or
account announces it (`routing/priority.go`), and requests of a lower priority
leave those alone (`held_for_priority`) until it has been served. It never
changes which model a key may use or how models are scored, and it does not
stop requests already running. A held request waits at most the normal queue
window (`NEOROUTER_ROUTING_QUEUE_WAIT_MS`).

## Budget pacing

A key with a daily or monthly budget is paced (`internal/budget/pacing.go`):
the spend so far plus the recent rate (this period blended with the last week)
forecasts the spend at the reset. Over pace, the cost weight grows (up to 4×) and
paid models whose typical request would take far more than its fair share of
what is left are ranked last; well under pace, the cost weight shrinks so better
models win. One-time budgets only get the per-request share. Responses carry
`X-NeoRouter-Budget-Pace` (`ahead`, `on_track`, `tight`, `over`).

## Prepaid plans

An account can be a prepaid plan (`prepaid` 1) or a plan followed by pay per
use (`prepaid` 2); `billing_mode` stays `paid`. Plan models cost nothing extra,
so they do not draw from key budgets and free-only keys may use them, but they
are never hedged (that would spend the allowance twice). `routing/plans.go`
reads the allowance from the provider's quota probe (`plan_requests`, or
percentage windows `plan_5h` / `plan_week`), subtracts requests sent since the
probe and uses the tightest window: a mild preference while there is room
(the allowance does not roll over), held back below 15 %, and once used up
either skipped (`plan_used_up`) or billed like any paid model. Plan accounts
are probed every 5 minutes and when they are added.
