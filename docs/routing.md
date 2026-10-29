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

## Other kinds

Embeddings, images, speech, transcription, System One (moderation), video
and web search use the same plan, learning, quotas and budgets
(`server/capabilities/shared/run_capability.js`); only the adapter method and
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
  `unsafe\nS9`, JSON verdicts); `server/capabilities/media/system_one.js` maps
  them to OpenAI moderation results. `check` picks `safety` (default),
  `prompt_injection` or `topic` (with a `policy`).
- **Video:** jobs stay bound to the provider that accepted them and are polled
  on `GET /v1/videos/:id`.
- **Web search:** each search engine is one `search` model
  (`server/providers/definitions/search.js`). Chat requests that ask for
  built-in search (`web_search_options`, OpenRouter's `plugins: [{ id: 'web' }]`
  or a `:online` model, a Responses `web_search` tool, an Anthropic
  `web_search_*` tool) get a `web_search` function; NeoRouter runs its calls
  through the search engines (up to 5 per request, `max_uses` up to 20, domain
  filters honoured) and loops until the model answers
  (`server/capabilities/chat/web_search.js`). Without a search engine the
  request runs as before. Queries pass the privacy shield again first.
- **Quality:** image, speech and video models are scored by Artificial
  Analysis arena ELO; other kinds by size and age within their kind.
- **Prices:** per image, per second of video, per minute of audio or per
  million characters (`price_unit`), or per token where the provider bills
  that way.

## Several accounts of one provider

Each account's copy of a model is its own candidate with its own stats,
cooldowns and quota pool. Copies of the same model compete only on health,
allowance left and free capacity (`spreadAcrossAccounts` in `router.js`), so
accounts wear evenly without changing which model wins. Concurrency limits are
learned (`concurrency.js`): a 429 while several requests run on an account sets
its limit below that count, steady success at the limit probes one higher, and
learned limits fade after an hour. An account at its limit is skipped
(`account_busy`) and chat requests wait briefly if every copy is busy.
