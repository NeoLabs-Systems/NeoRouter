'use strict';

// GET /api: the gateway reference as Markdown, written for coding agents and
// LLM tools that read it directly. Examples carry this server's own origin.
// scripts/build_api_docs.js publishes the same text to GitHub Pages.
const express = require('express');
const { getConfig } = require('../config/env');
const capabilities = require('../core/capability_registry');
const profiles = require('../services/routing/profiles');
const { TASK_KINDS } = require('../services/routing/task/classifier');

const router = express.Router();

const originOf = (req) => (getConfig().publicUrl || `${req.protocol}://${req.get('host')}`).replace(/\/+$/, '');

/** placeholder: the origin stands in for the reader's own server (static copies). */
function reference(origin, { placeholder = false } = {}) {
  const base = `${origin}/v1`;
  const wsBase = origin.replace(/^http/i, 'ws');
  const kinds = capabilities.list().map(({ id, label }) => `\`${id}\` (${label})`).join(', ');
  const presets = Object.keys(profiles.PRESETS).map((slug) => `\`${slug}\``).join(', ');
  const taskKinds = TASK_KINDS.map((kind) => `\`${kind}\``).join(', ');
  const aliases = Object.entries(profiles.ALIASES).map(([alias, slug]) => `\`${alias}\` → \`${slug}\``).join(', ');
  return `# NeoRouter API

NeoRouter is a self-hosted inference router. One endpoint speaks the OpenAI
Chat Completions, OpenAI Responses and Anthropic Messages APIs, plus
OpenAI-style embeddings, images, speech, transcription, moderation, video and
web search. It routes each request to one of the providers its owner has
connected and falls back to another model when one fails.

- Base URL: \`${base}\`
- Model kinds on this server: ${kinds}
- This document: \`GET ${origin}/api\` (Markdown)
${placeholder ? `- Examples use \`${origin}\`. Replace it with the address of your NeoRouter server.\n` : ''}
## Quick start

1. Get a router key (\`nrt_…\`): create one in the web app at \`${origin}/app/\`
   (Keys), or let your app ask for one with OAuth (see "Getting a key from an app").
2. Point any OpenAI or Anthropic SDK at \`${base}\` with that key.
3. Use \`"model": "auto"\` unless you need a specific model.

\`\`\`bash
curl ${base}/chat/completions \\
  -H "Authorization: Bearer $NEOROUTER_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{"model": "auto", "messages": [{"role": "user", "content": "Hello"}]}'
\`\`\`

\`\`\`python
import os
from openai import OpenAI
client = OpenAI(base_url="${base}", api_key=os.environ["NEOROUTER_KEY"])
reply = client.chat.completions.create(model="auto", messages=[{"role": "user", "content": "Hello"}])
\`\`\`

\`\`\`python
import os
from anthropic import Anthropic
client = Anthropic(base_url="${origin}", api_key=os.environ["NEOROUTER_KEY"])  # the SDK appends /v1
reply = client.messages.create(model="auto", max_tokens=1024, messages=[{"role": "user", "content": "Hello"}])
\`\`\`

## Authentication

Every \`/v1\` request needs a router key. Send it the way your SDK does; all
of these work:

- \`Authorization: Bearer nrt_…\` (OpenAI SDKs)
- \`x-api-key: nrt_…\` (Anthropic SDKs)
- \`x-goog-api-key: nrt_…\` (Gemini tooling)

A missing or wrong key gives \`401\` with code \`INVALID_API_KEY\`.

A key carries its own policy, set by its owner: which kinds of model it may use
(free, prepaid plans, paid), a USD budget (total, monthly or daily), a
requests-per-minute limit, a token limit for free and plan models, allowed
model patterns, a default routing style and an expiry date. Requests outside
that policy are refused or routed around; you cannot widen it from the request.

## Choosing a model

The \`model\` field accepts:

| Value | Meaning |
|---|---|
| \`auto\` | Let NeoRouter pick, using the key's default routing style. Also \`auto:default\`. |
| \`auto:<style>\` | Pick with a named style: ${presets}, or a custom style's name. Older names still work: ${aliases}. |
| \`auto-free\`, \`auto-plan\`, \`auto-paid\` | Like \`auto\`, but only free models, only models a subscription already covers, or only paid models. Combine with a built-in style: \`auto-free:fast\`. Custom styles already choose their kinds of model, so they have no such variants. |
| \`provider-slug/model-id\` | That exact model first; the same model on the owner's other providers is the fallback. |
| \`model-id\` | Every connected provider that serves this model. |

\`GET ${base}/models\` lists exactly what this key may use, including every
\`auto…\` variant. Do not hardcode model ids from elsewhere: the catalog
depends on which providers the owner connected.

Within \`auto\`, the router scores models on intelligence, measured speed,
measured reliability and cost, skips models that are rate limited, out of
quota, too small for the request or missing a needed feature (tools, vision),
and learns from every outcome.

## Response headers

| Header | Meaning |
|---|---|
| \`X-Request-Id\` | Id of this request. Send your own \`X-Request-Id\` to set it. |
| \`X-NeoRouter-Model\` | The model that answered (\`provider-slug/model-id\`). |
| \`X-NeoRouter-Trail\` | Every model tried, in order, comma-separated. More than one means a fallback happened. |
| \`X-NeoRouter-Task\` | Chat only: the kind of task the router saw and how hard it judged it, e.g. \`coding; difficulty=0.62\` (0 easy to 1 hard). Easy requests can go to smaller models that suffice. |
| \`X-NeoRouter-Budget-Pace\` | For keys with a daily or monthly budget: \`ahead\`, \`on_track\`, \`tight\` or \`over\`. |
| \`X-NeoRouter-Redacted\` | How many values the privacy shield hid from the provider (restored in the answer). |
| \`X-NeoRouter-Compression\` | Prompt compression level and estimated tokens saved, when enabled. |
| \`X-NeoRouter-Web-Searches\` | Number of web searches run for a chat request. |
| \`X-NeoRouter-Embedding-Pin\` | The embedding model an \`auto\` embedding key is pinned to. |
| \`X-NeoRouter-Audio-Format\` | The audio format actually returned by \`/audio/speech\`. |
| \`Retry-After\` | Seconds to wait, on \`429 KEY_RATE_LIMITED\`. |

Optional request header: \`X-NeoRouter-Compression: off | lossless | balanced | aggressive\`
lowers the owner's prompt compression level for this request (it can never raise it).

Optional request header for chat: \`X-NeoRouter-Task: <kind>\` names the task when you
know it better than the prompt shows. Kinds: ${taskKinds}. Unknown kinds are ignored.

## Chat

### POST /v1/chat/completions

OpenAI Chat Completions. Supports \`stream\`, \`tools\` / \`tool_choice\`,
images in content parts, \`response_format\`, \`max_tokens\`, \`temperature\`
and the other usual fields. Response and stream chunks have the OpenAI shape.

### POST /v1/messages

Anthropic Messages, including streaming, tools and images. Errors use the
Anthropic error shape. \`POST /v1/messages/count_tokens\` returns an estimate:
\`{"input_tokens": 123}\`.

### POST /v1/responses

OpenAI Responses API, including streaming and tools.

### Behaviour shared by all three

- **Fallback is transparent.** If a provider fails, times out or returns
  nothing usable, the next candidate is tried. Streams are only committed
  once the first real output arrives, so a failed attempt never leaks into
  your stream. Check \`X-NeoRouter-Trail\` to see what happened.
- **Tool calls are checked.** Malformed tool-call JSON, or an answer without
  a tool call when \`tool_choice\` requires one, counts as a failure and is retried elsewhere.
- **Built-in web search.** Ask for search the way your API does it
  (\`web_search_options\`, OpenRouter's \`plugins: [{"id": "web"}]\` or a
  \`:online\` model, a Responses \`web_search\` tool, an Anthropic
  \`web_search_*\` tool). If the owner connected a search engine, NeoRouter
  runs the searches and loops until the model answers. Without one, the
  request runs without search.
- **Privacy shield.** If the owner enabled it, secrets and personal data are
  replaced by placeholders before the request leaves, and put back in the answer.

## Models

### GET /v1/models

Lists the models this key may use, \`auto\` variants first. Add
\`?type=<kind>\` to list another kind (\`embedding\`, \`image\`, \`speech\`,
\`transcription\`, \`moderation\`, \`video\`, \`search\`, \`live\`); the default is \`chat\`.
With an \`anthropic-version\` header the list uses the Anthropic shape.

Each model has the OpenAI fields plus:

- \`context_length\`
- \`pricing.prompt\` / \`pricing.completion\`: USD per token as strings; \`"0"\`
  for free and plan models, \`null\` when unknown
- \`neorouter\`: \`kind\`, \`free\`, \`plan\`, \`intelligence\` (0–1), \`tools\`,
  \`vision\`, and \`unit_price\` / \`price_unit\` for kinds priced per image, second,
  minute or character. Live models list \`protocols\`; moderation models say
  whether they are \`decision\` models.

The list also carries \`neorouter.not_allowed_for_key\`: how many models of this
kind exist but are outside the key's free/plan/paid choice. Use it to tell
"nothing connected" from "not allowed for this key".

### GET /v1/models/{model}

One chat model, or \`404 MODEL_NOT_FOUND\`.

## Embeddings

### POST /v1/embeddings

\`\`\`json
{"model": "auto", "input": ["first text", "second text"], "dimensions": 768, "encoding_format": "float", "input_type": "document"}
\`\`\`

- \`input\`: a string or up to 2048 strings.
- \`input_type\` (NeoRouter extension): \`query\` or \`document\` (also
  \`passage\`, \`search_query\`, \`search_document\`) for retrieval models that embed them differently.
- **Pinning:** with \`auto\`, the key is pinned to the first model and size it
  used, because vectors of different models cannot be compared. Later requests
  only move between hosts of that same model. \`GET /v1/embeddings/pin\` shows
  the pin, \`DELETE /v1/embeddings/pin\` resets it (then re-embed your data).
  Asking for other \`dimensions\` than the pin gives \`400 EMBEDDING_PIN_MISMATCH\`.

## Images

### POST /v1/images/generations

\`\`\`json
{"model": "auto", "prompt": "a lighthouse at dawn", "n": 1, "size": "1024x1024", "response_format": "b64_json"}
\`\`\`

\`n\` is 1–4. \`response_format\` is \`b64_json\` (default) or \`url\`. URLs
point at \`${base}/media/{id}\`, need no key and expire.

## Speech

### POST /v1/audio/speech

\`\`\`json
{"model": "auto", "input": "Hello there", "voice": "alloy", "response_format": "mp3", "speed": 1.0}
\`\`\`

Returns audio bytes. \`input\` is at most 4096 characters. \`response_format\`:
\`mp3\`, \`opus\`, \`aac\`, \`flac\`, \`wav\`, \`pcm\`.

## Transcription

### POST /v1/audio/transcriptions and POST /v1/audio/translations

\`multipart/form-data\` with a \`file\` field (at most 25 MB) and optional
\`model\`, \`language\`, \`prompt\`, \`temperature\`, \`response_format\`
(\`json\`, \`text\`, \`srt\`, \`vtt\`, \`verbose_json\`). Translations return English text.

\`\`\`bash
curl ${base}/audio/transcriptions -H "Authorization: Bearer $NEOROUTER_KEY" \\
  -F file=@meeting.mp3 -F model=auto -F response_format=srt
\`\`\`

## Moderation (System One)

### POST /v1/moderations

\`\`\`json
{"model": "auto", "input": "text to check", "check": "safety"}
\`\`\`

Returns OpenAI moderation results. \`check\` (NeoRouter extension) is
\`safety\` (default), \`prompt_injection\`, or \`topic\` together with a
\`policy\` that says which topics are allowed. \`input\` may be up to 32 texts.

### POST /v1/systemone

Typed decisions over the decision models of the connected providers.

\`\`\`json
{"model": "auto", "state": {"…": "…"}, "questions": {"…": "…"}}
\`\`\`

Returns \`{"model": "…", "answers": …, "usage": …}\`.

## Video

Video generation is asynchronous.

- \`POST /v1/videos\` with \`{"model": "auto", "prompt": "…", "seconds": 4, "size": "1280x720"}\`
  (\`seconds\` 1–60) returns a job: \`{"id", "object": "video", "status", "progress", …}\`.
- \`GET /v1/videos/{id}\` refreshes and returns the job. Poll it every few seconds until \`status\` is \`completed\` or \`failed\`.
- \`GET /v1/videos/{id}/content\` streams the finished video.
- \`GET /v1/videos?limit=20\` lists recent jobs.

## Web search

### POST /v1/search

\`\`\`json
{"model": "auto", "query": "latest node.js release", "max_results": 5, "include_domains": [], "exclude_domains": [], "recency": "week"}
\`\`\`

\`max_results\` is 1–20; \`recency\` is \`day\`, \`week\`, \`month\` or \`year\`.
Returns \`{"object": "search", "model", "query", "results": [{"title", "url", "snippet", "content", "published"}]}\`.

## Live voice (WebSocket)

Connect to the same path you would use at the provider, with a router key
instead of the provider's. Messages pass through unchanged; only the model name
is rewritten. The model is chosen when the session opens and stays fixed.

| Protocol | URL | Model comes from |
|---|---|---|
| OpenAI Realtime | \`${wsBase}/v1/realtime?model=auto\` | the \`model\` query parameter |
| OpenAI Live | \`${wsBase}/v1/live/sessions\` | \`session.model\` in the first \`session.start\` event |
| Gemini Live | \`${wsBase}/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent\` | \`setup.model\` in the first message |

Browsers that cannot set headers can pass the key as \`?key=nrt_…\` or as the
\`openai-insecure-api-key.nrt_…\` subprotocol. \`GET /v1/models?type=live\` shows
which protocols each model speaks.

## Errors

OpenAI-style endpoints return:

\`\`\`json
{"error": {"message": "Readable explanation.", "type": "invalid_request_error", "code": "NO_MODEL_AVAILABLE", "details": {}}}
\`\`\`

\`/v1/messages\` returns \`{"type": "error", "error": {"type", "message"}, "request_id"}\`.
A failure after a stream started arrives as an \`error\` event in the stream.

| Status | Code | What to do |
|---|---|---|
| 400 | \`INVALID_REQUEST\`, \`INVALID_JSON\` | Fix the request body. |
| 400 | \`CONTEXT_TOO_LARGE\` | The input is larger than every model this key may use. Shorten it. |
| 400 | \`UNKNOWN_PROFILE\` | The \`auto:<style>\` name does not exist. Check \`GET /v1/models\`. |
| 400 | \`EMBEDDING_PIN_MISMATCH\` | Use the pinned dimensions or reset the pin. |
| 401 | \`INVALID_API_KEY\` | Send a valid \`nrt_…\` key. |
| 402 | \`BUDGET_EXHAUSTED\` | The key's budget is used up. Wait for the period to reset or ask the owner. |
| 403 | \`MODEL_NOT_ALLOWED\` | The named model is outside the key's policy. Use \`auto\` or a listed model. |
| 404 | \`MODEL_NOT_FOUND\` | The model is not in the catalog. Use \`GET /v1/models\`. |
| 413 | \`BODY_TOO_LARGE\`, \`FILE_TOO_LARGE\` | Send less. |
| 429 | \`KEY_RATE_LIMITED\` | Wait \`Retry-After\` seconds. |
| 429 | \`TOKEN_LIMIT_REACHED\` | The key's token limit for free and plan models is used up for this period. |
| 502 | \`ALL_ATTEMPTS_FAILED\` | Every candidate failed. \`details.trail\` lists the attempts. Retry later. |
| 503 | \`NO_MODEL_AVAILABLE\` | No model can take the request right now (rate limits, quotas, missing features). The message says why; \`details.rejected\` counts the reasons. |
| 503 | \`EMBEDDING_PIN_UNAVAILABLE\` | No provider serves the pinned embedding model right now. |

Retry \`429\`, \`502\` and \`503\` with backoff. Do not retry \`400\`–\`404\` unchanged.

## Getting a key from an app (OAuth)

Apps can ask the owner for a router key instead of having it pasted in.
Discovery: \`GET ${origin}/.well-known/oauth-authorization-server\`.
Scopes: ${capabilities.list().map(({ id }) => `\`${id}\``).join(', ')} (space-separated).
The owner decides on the approval screen which kinds of model, budget and limits the key gets.

**Authorization code with PKCE (S256)** for apps with a browser:

1. Send the user to \`${origin}/oauth/authorize?response_type=code&client_id=…&redirect_uri=…&code_challenge=…&code_challenge_method=S256&scope=chat&state=…\`.
2. Exchange the returned \`code\`: \`POST ${origin}/oauth/token\` with
   \`grant_type=authorization_code\`, \`code\`, \`client_id\`, \`redirect_uri\`, \`code_verifier\`.

**Device flow** for CLIs and agents without a browser:

1. \`POST ${origin}/oauth/device/code\` with \`client_id\` (and optional \`client_name\`, \`scope\`).
   You get \`device_code\`, \`user_code\`, \`verification_uri_complete\`, \`interval\`, \`expires_in\`.
2. Show the user \`verification_uri_complete\` (or \`user_code\`).
3. Poll \`POST ${origin}/oauth/token\` with
   \`grant_type=urn:ietf:params:oauth:grant-type:device_code\`, \`device_code\`, \`client_id\`
   every \`interval\` seconds. \`authorization_pending\` means keep waiting;
   \`slow_down\` means add 5 seconds; \`access_denied\` / \`expired_token\` mean stop.

Both return \`{"access_token": "nrt_…", "token_type": "Bearer", "scope", "base_url": "${base}", "key_id"}\`.
Use \`access_token\` as the router key against \`base_url\`.

## Other endpoints

- \`GET ${origin}/health\`: liveness and version, no key needed.
- \`GET ${origin}/ready\`: database readiness, no key needed.
- \`${origin}/api/v1/…\`: the web app's management API (providers, keys,
  usage). It needs a signed-in user session, not a router key. Integrations
  should use \`/v1\`.
`;
}

router.get('/api', (req, res) => {
  res.type('text/markdown; charset=utf-8');
  res.send(reference(originOf(req)));
});

module.exports = router;
module.exports.reference = reference;
