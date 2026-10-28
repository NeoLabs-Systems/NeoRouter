# NeoRouter — Engineering Guidelines

## Project Overview

NeoRouter is a Node.js/Express backend + Flutter web frontend. Users add
inference providers, NeoRouter validates the keys, imports every model into a
catalog and exposes one gateway (`/v1`) that speaks OpenAI Chat Completions,
Anthropic Messages and OpenAI Responses. Requests name a model
(`provider-slug/model-id`) or `auto[:profile]`, which the learning router
resolves.

### Directory Map

```
server/
  capabilities/chat/   Canonical format (OpenAI chat), translators, executor, gateway routes
  core/                Capability registry
  providers/           Registry, data-only definitions, protocol adapters, HTTP/SSE, error classes
  services/
    auth/              Users, sessions, registration gate
    providers/         Provider accounts (encrypted keys, validation)
    catalog/           Model sync, free classifier, model identity, metadata sources
    quota/             Sliding windows, header parser, learned limits
    routing/           Profiles, scoring, stats store, learning, load tracker, router
    budget/            Reservations and ledger
    router_keys/       Gateway keys (hash only), RPM limiter
    usage/             Request metadata log and aggregation
  workers/             Scheduler (key checks, model sync, metadata, quota probes)
  routes/              Management API (/api/v1), one file per domain
  db/                  SQLite via better-sqlite3, numbered migrations
runtime/               Paths and environment loading
lib/ bin/              CLI (`neorouter …`)
flutter_app/           Flutter web dashboard
landing/               Public website at /
scripts/               l10n string source
test/                  node:test unit and API tests, mock provider
```

## Core Principles

- **No duplication.** Search `server/utils/`, `runtime/`, and the relevant
  service directory before adding a helper.
- **No hardcoded models.** Behaviour comes from definitions, metadata and measurements.
- **No speculative features.** Implement exactly what is required.
- **Readable over clever.**

## Node.js / Server

- CommonJS. Every server file starts with `'use strict';`.
- Routes handle HTTP only. Services own business logic.
- All DB access goes through `server/db/database.js`; parameterized statements only.
- Schema changes belong in `server/db/migrations/` (append-only, numbered).
- Provider HTTP happens only inside `server/providers/adapters/`.
- Upstream failures are `ProviderError`s with a `kind`; routing reacts to kinds,
  never to raw status codes.

## Flutter / Dart

- `flutter_app/lib/main_controller.dart` is the single root `ChangeNotifier`.
- API calls go through `lib/src/router_api.dart`. Widgets stay thin.
- Never hardcode colors — use `neoRouterPaletteOf(context)`.
- All UI text goes through `AppL10n` (de + en).

## Naming

| Context | Convention |
|---|---|
| JS filenames | `snake_case` |
| JS variables / functions | `camelCase` |
| JS classes | `PascalCase` |
| Dart filenames | `snake_case` |
| Environment variables | `SCREAMING_SNAKE_CASE` |
| HTTP route paths | `kebab-case` |
| SQLite tables / columns | `snake_case` |

## Hard Rules

- One SQLite database file. No second store.
- Do not commit `.env`, runtime data, or anything matched by `.gitignore`.
- Do not run `flutter build web` during normal development.
- Do not invent custom cryptography when Node `crypto` already provides it.
