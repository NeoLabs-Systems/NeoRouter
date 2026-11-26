# NeoRouter — Engineering Guidelines (Go beta)

## Project Overview

NeoRouter is a Go backend + Flutter web frontend. Users add inference providers,
NeoRouter validates the keys, imports every model into a catalog and exposes one
gateway (`/v1`) that speaks OpenAI Chat Completions, Anthropic Messages and OpenAI
Responses. Requests name a model (`provider-slug/model-id`) or `auto[:profile]`,
which the learning router resolves.

Generic building blocks shared by the NeoLabs apps (sealed secrets, password
hashing, TOTP, rate limits, SSRF-safe HTTP, LLM provider clients, the CLI shell,
release channels, service supervision) live in the separate module
[`github.com/NeoLabs-Systems/neolabs-go`](https://github.com/NeoLabs-Systems/neolabs-go).
NeoRouter-specific code (its tables, routes, routing math, provider definitions)
lives here.

### Directory Map

```
cmd/
  neorouter/           The single binary: server (`neorouter serve`) and CLI (install, update, ...)
  neorouter-release/   Release tooling used by CI (runtime archives, manifests)
internal/
  config/              Environment and paths (NEOROUTER_HOME, data dir, web dir)
  schema/              The database schema: baseline.sql plus ordered Go steps
  secrets/             Sealing stored secrets and the key file
  httpserver/          HTTP shell: request ids, CORS, error envelope, compression, static clients
  app/                 Builds every domain and connects them through small interfaces
  auth/ teams/         Accounts, sessions, 2FA; team spaces and permissions
  providers/           Registry, data-only definitions, protocol adapters, accounts, billing
  catalog/             Model sync, free classifier, model identity, metadata sources
  quota/ budget/       Sliding windows, header parser, learned limits; reservations and ledger
  routing/             Profiles, scoring, stats, learning, load tracker, plans, slots, groups, router
  routerkeys/ usage/   Gateway keys (hash only), RPM/concurrency; request metadata log and aggregation
  gateway/ media/      /v1 chat surface (canonical format, translators, executor); other capabilities
  live/ pools/         WebSocket voice relay; federation and pool relay
  processing/          Shield rules engine, redaction, compression, local privacy filter (ONNX)
  localmodels/         Local inference runtimes (llama.cpp, ONNX embeddings)
  discovery/ apps/     News and benchmarks; connected apps and "Connect with NeoRouter" OAuth
  oauth/ proxies/      Provider OAuth flows; outbound SOCKS proxy rotation
  status/ updates/     Public status + history + analytics; release channels and update checks
  scheduler/           Jittered background workers (key checks, model sync, metadata, quota probes)
  settings/            Global and per-user settings, account data export/delete
  manager/ backup/     The CLI (install, update, service, doctor, migration) and DB snapshots
flutter_app/           Flutter web dashboard (served at /app)
landing/               Public website at /
scripts/               l10n string source and data pipelines
```

## Core Principles

- **Assume another machine.** NeoRouter usually runs on a different server; logs a
  user supplies may not match files on this computer.
- **One visual language.** Reuse NeoLabs design tokens, auth UX, landing structure
  and backend conventions.
- **No duplication.** Search the domain package, `internal/`, and `neolabs-go`
  before adding a helper. Reuse what exists.
- **No hardcoded models.** Behaviour comes from definitions, metadata and measurements.
- **No speculative features.** Implement exactly what is required.
- **No dead code.** Remove unused functions, variables, and branches.
- **Readable over clever.** Prefer direct, boring code.
- **Generic goes to the foundation.** A building block that knows nothing about
  NeoRouter's schema or product belongs in `neolabs-go` (with tests).

## Go / Server

### Packages

- One package per domain under `internal/`. A domain exposes struct-literal
  services (dependencies as fields: `DB *dbx.DB`, `Cfg func() config.Config`,
  `Log *slog.Logger`, narrow func fields for cross-domain calls) and
  `Routes(mux *http.ServeMux)` with Go 1.22 patterns such as
  `"GET /api/v1/models"`. Optional `Start(ctx)` / `Stop()` for background work.
- **Domains do not import each other.** A domain that needs another declares the
  narrow interface it uses in its own package (consumer side); `internal/app`
  satisfies it. `cmd/neorouter` stays thin.
- Handlers handle HTTP only: parse the request, call the service, write the
  response with `httpx.WriteJSON`. Business logic lives in the service.
- Split files that grow past ~400 lines along real seams.

### API compatibility

- The Flutter client, connected apps, pool peers and existing installs depend on
  the API: keep routes, status codes, error texts, key order, `null` versus
  omitted keys, and date formats stable. Error texts are part of the contract.
- Management API errors: `{"error":{"code","message","details"?,"requestId"}}`.
  Gateway errors: the OpenAI shape `{"error":{"message","type","code"}}`.
- Every request that changes behaviour visible to clients needs a test that pins
  the response.

### Database

- One SQLite database (`modernc.org/sqlite`, pure Go), opened once through `dbx`
  (writer pool + reader pool). Never open a second handle from a domain.
- All queries use parameters. No string interpolation into SQL.
- The schema is `internal/schema/baseline.sql` (the schema existing installs already
  have) plus ordered steps in `schema.Steps()`. A schema change is a new step; never
  edit the baseline or create tables inline in a domain.
- Opening a database runs `schema.Ensure`: fresh DBs get the baseline, existing
  Node DBs sync `PRAGMA user_version` from `schema_migrations` and are verified.
  Tests use a temp `NEOROUTER_HOME`; never point tests at `~/.neorouter`.

### Providers and routing

- Provider HTTP happens only inside `internal/providers` (adapters) through
  `neolabs-go/llm` clients and `ssrf`-guarded transports.
- Upstream failures are typed provider errors with a `Kind`; routing reacts to
  kinds, never to raw status codes.
- A new provider is an entry in `internal/providers/definitions.json` (keep the
  registry tests in step); a new capability (search, image, audio, video) is a new
  adapter registered through the gateway capability registry.
- Routing must stay data-driven: metadata sources, measured stats and provider
  definitions. Centralize thresholds in `internal/config` (env-backed).
- "Free" means 100 % free. Never classify an unknown price as free.

### Error handling

- Validate at system boundaries: request bodies, external API responses,
  user-supplied env values. Do not defensively validate internal arguments.
- Return errors with context (`fmt.Errorf("routing: plan: %w", err)`); log once,
  where the error is handled, with `slog`.

### Security

- Secrets are sealed at rest with `secretbox` (`enc:v1:`); legacy Node-era
  values are re-sealed by the manager migration. Store token digests, not
  tokens; compare secrets in constant time (`passhash`).
- Every outbound request a user can influence goes through `neolabs-go/ssrf`;
  private destinations stay admin-only unless explicitly allowed.
- Data directories are 0700, files 0600. Never log secrets, tokens, prompts or
  completions — usage telemetry carries metadata only.
- CORS and origin validation go through `internal/httpserver`. No ad-hoc bypasses.

### Environment & paths

- Read configuration through `internal/config`. Never hardcode `~/.neorouter`
  or absolute system paths.

### Tests

- `gofmt`, `go vet ./...`, `staticcheck ./...`, and `go test -race ./...` must
  pass (CI runs them).
- Use a real temp SQLite (`dbx.Open` + `schema.Ensure`) and `httptest`, not mocks
  of the database. Table-driven tests for behaviour.

## Flutter / Dart

- `flutter_app/lib/main_controller.dart` is the single root `ChangeNotifier`.
- API calls go through `lib/src/router_api.dart`. Widgets stay thin.
- Never hardcode colors — use `neoRouterPaletteOf(context)`.
- All UI text goes through `AppL10n` (de + en).
- Shared auth/settings widgets come from `neolabs-flutter` where they render the
  same thing; do not fork a second visual language.
- Follow `flutter_app/analysis_options.yaml`. Do not run `dart format` on whole
  files. Do not run `flutter build web` during normal development (the release
  pipeline owns it). CI pins Flutter 3.47.9.

## Releases and packaging

- NeoRouter ships as one Go binary plus a signed runtime archive laid out as
  `app/bin/neorouter`, `app/flutter_app/build/web` and `app/landing`. Running
  it needs no Node.js, npm or Git.
- `package.json` exists only so Node.js installations can hand over to the Go
  version; the release workflows keep its version in step.
- `flutter_app/build/web` is generated: CI builds and commits it. Never edit or
  commit it by hand.
- CI steps shared by the NeoLabs apps come from `neolabs-go/.github/actions`,
  pinned to one commit.

## Naming

| Context | Convention |
|---|---|
| Go packages | short, lowercase, no underscores (`routerkeys`, `localmodels`) |
| Go files | `snake_case.go` |
| Go identifiers | `camelCase` / `PascalCase` for exported |
| Dart filenames | `snake_case` |
| Environment variables | `SCREAMING_SNAKE_CASE` |
| HTTP route paths | `kebab-case` |
| SQLite tables / columns | `snake_case` |

## Hard Rules

- One SQLite database file. No second store.
- Do not call AI provider APIs outside `internal/providers` and `neolabs-go/llm`.
- Do not commit `.env`, runtime data, or anything matched by `.gitignore`.
- Do not add fake implementations or unfinished placeholder behaviour.
- Do not create `CHANGELOG.md`; GitHub Releases are the canonical release history.
