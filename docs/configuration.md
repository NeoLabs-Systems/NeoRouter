# Configuration

NeoRouter reads `~/.neorouter/.env` (created by `neorouter install`) and the
process environment. Change values with `neorouter env set KEY value`.

## Server

| Variable | Default | Meaning |
| --- | --- | --- |
| `NEOROUTER_HOST` | `127.0.0.1` | Bind address. Use `0.0.0.0` behind a reverse proxy. |
| `NEOROUTER_PORT` | `4600` | HTTP port for `/`, `/app`, `/api/v1` and `/v1`. |
| `NEOROUTER_PUBLIC_URL` | – | External URL; used for the endpoint shown in the app, OAuth callbacks, and the address pool partners reach. Without one, a NeoRouter can still join a pool: it takes part through the pool's host. |
| `NEOROUTER_HOME` | `~/.neorouter` | Data, logs, backups and `secret.key`. `/data` in the container image. |
| `NEOROUTER_CONTAINER` | auto | Forces the container behaviour (no service install, foreground start). Detected from `/.dockerenv` and Kubernetes env otherwise. See [docker.md](docker.md). |
| `NEOROUTER_TRUST_PROXY` | `false` | Trust `X-Forwarded-*` from a reverse proxy. |
| `NEOROUTER_ALLOW_PRIVATE_PROVIDER_URLS` | `false` | Let every user, not only the instance admin, use local and private network addresses for provider accounts. |
| `NEOROUTER_ALLOWED_ORIGINS` | – | Extra CORS origins (comma separated); loopback is always allowed. |
| `NEOROUTER_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. |
| `NEOROUTER_SESSION_TTL_MS` | 30 days | Dashboard session lifetime. |
| `NEOROUTER_RELEASE_CHANNEL` | `stable` | Release channel `neorouter update` follows (`stable` or `beta`). Set it with `neorouter channel`. |
| `NEOROUTER_DATABASE_PATH` | `data/neorouter.sqlite3` | Location of the database. |
| `NEOROUTER_MAX_BODY_BYTES` | 32 MiB | Largest request body accepted. |
| `NEOROUTER_WEB_DEV_URL` | – | Development only: redirect `/app` to a running `flutter run -d web-server`. |

## Email

Optional. With an SMTP server, a sender and a public address set, the sign-in
page offers **Forgot password?**. Without them, an admin resets a password with
`neorouter reset-password`.

| Variable | Default | Meaning |
| --- | --- | --- |
| `NEOROUTER_EMAIL_SMTP_HOST` | – | SMTP server. |
| `NEOROUTER_EMAIL_FROM` | – | Sender address. |
| `NEOROUTER_EMAIL_PUBLIC_URL` | `NEOROUTER_PUBLIC_URL` | Address used for links in mails. |
| `NEOROUTER_EMAIL_SMTP_PORT` | `587` | Port `465` uses implicit TLS. |
| `NEOROUTER_EMAIL_SMTP_USER` / `NEOROUTER_EMAIL_SMTP_PASS` | – | SMTP sign-in. |
| `NEOROUTER_EMAIL_REPLY_TO` | – | Reply address. |

Passkeys and security keys need the app to be opened over HTTPS or on
`localhost`.

## Router key expiration

In **API keys → Create key / Edit**, choose no expiration, 7/30/90 days, or a
custom date and time. The form and key list show the exact cutoff in the
browser's local time zone, including its UTC offset. Existing keys keep their
current expiration unless you explicitly change it. The same choice is available
when approving a connected app; expiring OAuth tokens include `expires_in`.

Expired keys are listed separately and can be renewed through **Renew / edit**.
Extending or removing expiry reuses the existing secret; it never reactivates a
revoked key. At expiration, new gateway requests receive HTTP 401. Requests that
were already authenticated are allowed to finish.

The management API accepts `expiresAt` as an ISO 8601 timestamp with `Z` or an
explicit UTC offset on `POST /api/v1/keys` and `PATCH /api/v1/keys/:id`. Use `null`
to remove expiry; omit the field on updates to preserve it. New or changed
deadlines must be in the future. Key responses include `expiresAt` and `expired`.

## Routing

| Variable | Default | Meaning |
| --- | --- | --- |
| `NEOROUTER_UPSTREAM_TIMEOUT_MS` | `120000` | Per-attempt timeout (and first-token timeout for streams). |
| `NEOROUTER_ROUTING_MAX_ATTEMPTS` | `8` | Attempts per request, including fallbacks and hedges. |
| `NEOROUTER_ROUTING_TOTAL_BUDGET_MS` | `300000` | Hard limit for one request. |
| `NEOROUTER_ROUTING_TOP_K` | `5` | Candidates the load-spreading draw picks from. |
| `NEOROUTER_ROUTING_HEDGING` | `true` | Race a second free model when one is slower than usual. |
| `NEOROUTER_ROUTING_MAX_PARALLEL` | `3` | Attempts that may run at once for one request. |
| `NEOROUTER_ROUTING_HEDGE_UNMEASURED_MS` | `10000` | Hedge delay for models without latency history. |
| `NEOROUTER_ROUTING_QUEUE_WAIT_MS` | `45000` | How long a request waits when every candidate is temporarily rate limited. |

## Background jobs

| Variable | Default | Meaning |
| --- | --- | --- |
| `NEOROUTER_WORKERS_ENABLED` | `true` | Key checks, model sync, metadata refresh and quota probes. |
| `NEOROUTER_VALIDATE_INTERVAL_MS` | 5 min | Key re-validation. |
| `NEOROUTER_MODEL_SYNC_INTERVAL_MS` | 2 h | Model list refresh. |
| `NEOROUTER_METADATA_INTERVAL_MS` | 1 h | models.dev / OpenRouter / Artificial Analysis refresh. |
| `NEOROUTER_QUOTA_PROBE_INTERVAL_MS` | 15 min | Balance and credit probes. |
| `NEOROUTER_METADATA_SOURCES_ENABLED` | `true` | Disable to run fully offline. |
