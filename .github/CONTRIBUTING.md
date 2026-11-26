# Contributing to NeoRouter

## Development

```bash
cd flutter_app && flutter pub get && cd ..
go run ./cmd/neorouter serve     # server on http://127.0.0.1:4600
cd flutter_app && flutter run -d chrome --dart-define NEOROUTER_API_BASE=http://127.0.0.1:4600
```

The backend is Go 1.27 (`cmd/neorouter`, `internal/`); the dashboard is Flutter
web. See [GUIDELINES.md](GUIDELINES.md) for the package layout, conventions and
product rules.

## Tests

```bash
gofmt -l cmd internal            # must be empty
GOWORK=off go vet ./...
GOWORK=off go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...   # see staticcheck.conf
GOWORK=off go test -race ./...   # real temp SQLite + httptest, no network
cd flutter_app && flutter analyze
cd flutter_app && flutter test
```

The Go tests pin the HTTP API byte for byte: existing clients depend on it.

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

`beta` is the default branch; `main` is stable. Every push to `beta` publishes a
beta release; **Release Stable** (run by hand) promotes `beta` into `main` and
publishes it. The release workflows build the dashboard, commit it with the new
version (`internal/config/config.go` and `package.json`) and attach the CLI
binaries, the runtime archives and their signed manifest. A release stays a draft
until all of them are there.

`package.json` is not a Node project: Node.js installations read it during
`neorouter update` to find the way to the Go version.

Repository secrets:

| Secret | Used for |
| --- | --- |
| `NEOROUTER_RUNTIME_SIGNING_PRIVATE_KEY` | Signs the runtime manifest installs update from. Releases stop before tagging if it is missing or does not match `internal/manager/public_key.txt`. |
| `DISCORD_RELEASE_WEBHOOK_URL` | Release announcement. |

`go run ./cmd/neorouter-release runtime-keygen` writes a new key pair into
`dist/`: commit the printed public key and store the private key as the secret.
Installs only accept updates signed with the key they already carry.
