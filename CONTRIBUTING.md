# Contributing to NeoRouter

## Development

```bash
npm install
cd flutter_app && flutter pub get && cd ..
npm run dev                      # server on http://127.0.0.1:4600 (watch mode)
cd flutter_app && flutter run -d chrome --dart-define NEOROUTER_API_BASE=http://127.0.0.1:4600
```

## Tests

```bash
npm test                         # node:test unit + API tests (mock provider, no network)
npm run flutter:analyze
npm run flutter:test
```

## Strings

UI strings live in `scripts/l10n_strings.py` (English + German side by side).
Run `python3 scripts/l10n_strings.py` and `cd flutter_app && flutter gen-l10n`
after changing them. Never edit the `.arb` files by hand.

## Adding a provider

See `docs/providers.md`. Most providers are a single entry in
`server/providers/definitions/`.

## Web UI during development

Run the server with `NEOROUTER_WEB_DEV_URL=http://127.0.0.1:5699/` and the app
with `flutter run -d web-server --web-port 5699 --dart-define NEOROUTER_API_BASE=http://127.0.0.1:4600`;
`/app` then redirects to the dev server. Do not run `flutter build web` during
normal development — `neorouter rebuild-web` builds the bundled client.

## Release

`beta` is the default branch; `main` is stable.
