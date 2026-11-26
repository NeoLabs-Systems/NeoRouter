# Contributing to NeoRouter

## Development

```bash
cd flutter_app && flutter pub get && cd ..
go run ./cmd/neorouter serve     # server on http://127.0.0.1:4600
cd flutter_app && flutter run -d chrome --dart-define NEOROUTER_API_BASE=http://127.0.0.1:4600
```

The backend is Go 1.27 (`cmd/neorouter`, `internal/`); the dashboard is Flutter
web. See `GUIDELINES.md` for the package layout and conventions, and
`AGENTS.md` for the product rules.

## Tests

```bash
gofmt -l .                       # must be empty
go vet ./...
staticcheck ./...                # honnef.co/go/tools; see staticcheck.conf
go test -race ./...              # real temp SQLite + httptest, no network
cd flutter_app && flutter analyze
cd flutter_app && flutter test
```

The Go tests port the former Node `node:test` suites case for case; they are the
behavioural oracle for the byte-compatible HTTP API.

## Strings

UI strings live in `scripts/l10n_strings.py` (English + German side by side).
Run `python3 scripts/l10n_strings.py` and `cd flutter_app && flutter gen-l10n`
after changing them. Never edit the `.arb` files by hand.

## Adding a provider

See `docs/providers.md`. Most providers are a single data entry in
`internal/providers/definitions.json`; a new protocol dialect is an adapter in
`internal/providers/adapters/` built on `neolabs-go/llm`.

## Web UI during development

Run the server with `NEOROUTER_WEB_DEV_URL=http://127.0.0.1:5699/` and the app
with `flutter run -d web-server --web-port 5699 --dart-define NEOROUTER_API_BASE=http://127.0.0.1:4600`;
`/app` then redirects to the dev server. Do not run `flutter build web` during
normal development — `neorouter rebuild-web` builds the bundled client.

## Release

`beta` is the default branch; `main` is stable.