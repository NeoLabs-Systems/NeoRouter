# Docker

NeoRouter ships as a single image. It holds the server, the prebuilt Flutter
dashboard and the landing page; all state lives in one volume.

## Quick start

```bash
docker compose up -d --build
```

Then open **http://localhost:4600/app** — the first account you create is the
administrator. Add a provider, create a router key, and point any OpenAI or
Anthropic client at `http://localhost:4600/v1`.

Without Compose:

```bash
docker build -t neorouter .
docker run -d --name neorouter \
  -p 4600:4600 \
  -v neorouter-data:/data \
  -e NEOROUTER_PUBLIC_URL=http://localhost:4600 \
  --restart unless-stopped \
  neorouter
```

## The `/data` volume

Everything the process writes is under `/data` (image default
`NEOROUTER_HOME=/data`):

| Path | Contents |
| --- | --- |
| `/data/data/neorouter.sqlite3` | Users, provider accounts, keys, catalog, usage |
| `/data/data/secret.key` | AES-256-GCM key for provider API keys |
| `/data/data/models/` | Privacy-filter weights, when that feature is enabled |
| `/data/.env` | Configuration, seeded on first start from `docker/env.default` |
| `/data/logs/`, `/data/backups/` | Log files and pre-update backups |

Losing the volume means provider keys can no longer be decrypted: back it up
rather than recreating it. `neorouter backup` writes into `/data/backups`.

The container runs as uid/gid 1000. A **bind mount** must be owned by that uid:

```bash
mkdir -p ./neorouter-data
sudo chown -R 1000:1000 ./neorouter-data
docker run -d -v "$PWD/neorouter-data:/data" … neorouter
```

A named volume (`-v neorouter-data:/data`) gets the right ownership from the
image and needs nothing.

## Configuration

Process environment wins over `/data/.env`. The image sets:

| Variable | Image default | Why |
| --- | --- | --- |
| `NEOROUTER_HOME` | `/data` | Single writable location |
| `NEOROUTER_HOST` | `0.0.0.0` | The default `127.0.0.1` is unreachable through a published port |
| `NEOROUTER_PORT` | `4600` | Matches `EXPOSE` and the healthcheck |

`docker/env.default` (copied to `/data/.env` on first start) additionally sets
`NEOROUTER_RELEASE_CHANNEL=stable`. Every other setting is in
[configuration.md](configuration.md).

Change settings with compose `environment:` or `docker run -e`. `neorouter env
set` also works for anything the image does not set itself, but the file is
read at start, so restart after using it — and `NEOROUTER_HOME`, `NEOROUTER_HOST`
and `NEOROUTER_PORT` come from the image environment, which wins over the file.

### Behind a reverse proxy

```yaml
ports:
  - "127.0.0.1:4600:4600"     # not published to the internet itself
environment:
  NEOROUTER_PUBLIC_URL: https://router.example.com
  NEOROUTER_TRUST_PROXY: "true"
```

### Reaching a provider on the host

`localhost` inside the container is the container, not the host. Compose maps
`host.docker.internal` to the host gateway:

| Provider runs on the host | Base URL to enter in NeoRouter |
| --- | --- |
| Ollama | `http://host.docker.internal:11434/v1` |
| LM Studio | `http://host.docker.internal:1234/v1` |
| vLLM / llama.cpp server | `http://host.docker.internal:8000/v1` |

If the instance has more than one user, set
`NEOROUTER_ALLOW_PRIVATE_PROVIDER_URLS=true` — otherwise only the administrator
may point a provider at a private or loopback address.

## CLI

The image has `neorouter` on `PATH`:

```bash
docker compose exec neorouter neorouter status
docker compose exec neorouter neorouter doctor
docker compose exec neorouter neorouter reset-password admin 'a-long-password'
docker compose exec neorouter neorouter reset-2fa admin
docker compose exec neorouter neorouter providers import ./providers.json --user admin
docker compose exec neorouter neorouter backup
docker compose logs -f
```

`neorouter providers import` reads a file path, so copy it in first
(`docker cp providers.json neorouter:/tmp/`).

Inside a container the lifecycle commands defer to the container runtime
instead of pretending:

- `install`, `start` and `serve` run the server in the **foreground** — that is
  what the entrypoint does by default.
- `stop` prints `docker stop <container>`; `restart` therefore cannot work from
  inside and the container runtime restarts the process.
- `update` explains the image update instead of pulling git into a layer that
  the next `docker compose up` throws away.
- `repair` fixes dependencies and the database, then asks for a container
  restart for the service part.
- `status` reports `manager container` in place of launchd/systemd.

## Building

The default build downloads the dashboard that GitHub already built for the
branch (the `web-main` / `web-beta` release, 12 MB) instead of installing
Flutter. That is also what an npm install does without the Flutter SDK.

```bash
docker build -t neorouter .                                  # dashboard downloaded
docker build -t neorouter:beta --build-arg NEOROUTER_WEB_CHANNEL=beta .
docker build --target runtime-from-source -t neorouter:src .  # dashboard built with Flutter
```

`runtime-from-source` uses the `ghcr.io/cirruslabs/flutter` image and builds
`flutter_app` from the checkout — use it after changing the Flutter app, or
when the prebuilt release is not reachable. It is slow (multi-GB base image).

Build arguments:

| Argument | Default | Meaning |
| --- | --- | --- |
| `NODE_VERSION` | `22` | Node base image tag (must be 20 or newer) |
| `FLUTTER_VERSION` | `3.35.5` | Flutter SDK image for `runtime-from-source` |
| `NEOROUTER_WEB_CHANNEL` | `main` | Prebuilt client branch: `main` or `beta` |
| `NEOROUTER_WEB_URL` | – | Full URL (or `file://` path) to `neorouter-web.tar.gz`, for mirrors and air-gapped builds |
| `UID` / `GID` | `1000` | Runtime user, for bind mounts owned by somebody else |

Native modules (`better-sqlite3`, `bcrypt`) are compiled in the build stage and
verified there: a module without its binary fails the build instead of failing
at runtime.

## Operations

- **Healthcheck**: `GET /health` every 30 s, 40 s grace period. Compose
  `depends_on: condition: service_healthy` waits for it.
- **Shutdown**: `tini` is PID 1, so `SIGTERM` reaches the server, which closes
  the HTTP server and the scheduler cleanly. Allow ~20 s
  (`stop_grace_period`).
- **Logs**: JSON lines on stdout/stderr; `neorouter logs` is not useful here,
  `docker compose logs` is. Compose caps the json-file driver at 3 × 10 MB.
- **Single instance**: SQLite with WAL and a scheduler that assumes one writer.
  Do not scale this service to more than one replica.
- **ffmpeg** is in the image, so text-to-speech output is converted to the
  format the client requested.

## Updating

```bash
docker compose build --pull && docker compose up -d   # built from a checkout
docker compose pull && docker compose up -d           # published image
```

The volume is untouched. Migrations run at start — take a backup first if the
release notes mention schema changes:

```bash
docker compose exec neorouter neorouter backup
```
