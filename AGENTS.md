# Agent instructions

- NeoRouter is a standalone NeoLabs product. Reuse NeoLabs design tokens,
  auth UX, landing structure, and backend conventions. Do not invent a
  second visual language.
- The backend is Go (`cmd/neorouter`, `internal/`), following the same
  conventions as NeoAgent and NeoRecall on their beta branches. Generic
  building blocks come from `github.com/NeoLabs-Systems/neolabs-go`; search it
  before writing a helper. The Flutter dashboard stays visually identical and
  uses `neolabs-flutter` where it renders the same thing.
- Always expect the application to run on another server. Logs supplied by
  a user may not match files on this computer.
- Do not hardcode models or scenario-specific behaviour. Routing must stay
  data-driven: metadata sources, measured stats and provider definitions.
  Centralize thresholds (see `internal/routing/`, `internal/config/`).
- "Free" means 100 % free. Never classify an unknown price as free.
- Do not add fake implementations or unfinished placeholder behaviour.
- Never log or store prompts, completions, provider keys or router key secrets.
- The HTTP API, error texts and JSON shapes are a byte-compatible contract with
  the Flutter client, connected apps, pool peers and existing Node installs.
  Pin responses with tests.
- New provider = new entry in `internal/providers/definitions.json` (keep it in
  step with the registry tests); new capability (search,
  image, audio, video) = a new adapter surface registered through the gateway
  capability registry.
- Schema changes are new steps in `internal/schema/` after the
  baseline. Never edit `baseline.sql`; never create tables in a domain.
- Existing installs migrate: the manager re-seals legacy encrypted values to
  `secretbox` (`enc:v1:`), syncs `PRAGMA user_version` from `schema_migrations`,
  backs up the database first and writes `data/.go-migrated`.