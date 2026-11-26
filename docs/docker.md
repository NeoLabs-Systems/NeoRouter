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

## Choosing the version

`main` is the stable branch, `beta` the development branch. The image is built
from whatever you have checked out:

```bash
git switch main && docker compose up -d --build                            # stable
git switch beta && NEOROUTER_CHANNEL=beta docker compose up -d --build     # beta
git checkout v1.2.3 && docker compose up -d --build                        # one release
```

`NEOROUTER_CHANNEL` (`stable` by default) only sets the release channel the image
reports. Compose also reads it from a `.env` file next to `compose.yaml`.

Or skip the build and use a published image,
`ghcr.io/neolabs-systems/neorouter`:

| Tag | Follows |
| --- | --- |
| `latest`, `main` | The newest stable release |
| `beta` | The newest release, beta or stable |
| `1.2.3`, `1.2` | That version |

## Upgrading from the Node.js image

Keep your volume (or bind mount): the path stays `/data`, the user stays uid
1000, and the database, `secret.key` and `/data/.env` are used as they are. The
database is upgraded on the first start, after a snapshot is saved in
`/data/backups`. The Node.js image cannot read what the new server writes, so for
a clean way back **copy the volume before you upgrade**:

```bash
docker compose stop
docker run --rm --volumes-from neorouter -v "$PWD:/backup" debian:trixie-slim \
  tar -czf /backup/neorouter-data.tar.gz -C /data .
```

Going back to the old image means restoring that copy into an empty volume
first.

Then update the checkout and run `docker compose up -d --build`. An existing
compose file keeps working; these parts of it are now out of date:

| In an older setup | Now |
| --- | --- |
| `NEOROUTER_VERSION` | Removed and ignored. Check out the tag, or use a published image tag. |
| `target: runtime` or `runtime-from-source` | Both still accepted and identical. Remove the line. |
| `NODE_VERSION`, `FLUTTER_VERSION`, `NEOROUTER_WEB_URL`, `web-archive` context | Ignored: nothing is downloaded or compiled but the server. |
| `healthcheck` running `node -e …` | Still answered. Use `test: ["CMD", "neorouter", "healthcheck"]`. |
| Commands running `node` or `npm` | Gone. Use the `neorouter` command. |
| Channel `beta` by default | `stable` by default. |

## The `/data` volume

Everything the process writes is under `/data` (image default
`NEOROUTER_HOME=/data`):

| Path | Contents |
| --- | --- |
| `/data/data/neorouter.sqlite3` | Users, provider accounts, keys, catalog, usage |
| `/data/data/secret.key` | AES-256-GCM key for provider API keys |
| `/data/data/models/` | Privacy-filter weights, when that feature is enabled |
| `/data/.env` | Configuration, seeded on first start from `docker/env.example` |
| `/data/logs/`, `/data/backups/` | Log files and backups |

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
| `NEOROUTER_RELEASE_CHANNEL` | build channel | Reports the channel the image was built from |

`docker/env.example` is a complete, commented list of everything the server
reads. The entrypoint copies it to `/data/.env` on first start, so the defaults
are visible and editable from inside the container; the release channel line in
that copy is set from the channel the image was built for.

Change settings with compose `environment:` or `docker run -e`. `neorouter env
set` also works for anything the image does not set itself, but the file is
read at start, so restart after using it — and `NEOROUTER_HOME`, `NEOROUTER_HOST`,
`NEOROUTER_PORT` and `NEOROUTER_RELEASE_CHANNEL` come from the image
environment, which wins over the file.

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
docker compose exec neorouter neorouter backup list
docker compose logs -f
```

`neorouter providers import` reads a file path, so copy it in first
(`docker cp providers.json neorouter:/tmp/`).

`restore` replaces the data, so stop the server first and run it in a one-off
container:

```bash
docker compose stop
docker compose run --rm neorouter restore <backup>
docker compose up -d
```

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

The server is compiled in a `golang:1.27-trixie` stage into one static binary
(`CGO_ENABLED=0`, pure-Go SQLite) and runs on `debian:trixie-slim` with
`ffmpeg`, `libgomp1` (for local llama.cpp models) and `tini` as PID 1. The
dashboard (`flutter_app/build/web`) is copied from the checkout: releases build
and commit it, so the image never downloads it. If you changed the Flutter app,
or your checkout has no bundle yet, build it first:

```bash
cd flutter_app && flutter build web --release --no-tree-shake-icons --base-href /app/
```

| Build argument | Default | Meaning |
| --- | --- | --- |
| `NEOROUTER_CHANNEL` | `stable` | `stable` or `beta`: the release channel the image reports |
| `UID` / `GID` | `1000` | Runtime user, for bind mounts owned by somebody else |

## Operations

- **Healthcheck**: `neorouter healthcheck` (calls `GET /health`) every 30 s, 60 s grace period. Compose
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
git pull && docker compose up -d --build              # built from a checkout
docker compose pull && docker compose up -d           # published image
```

The volume is untouched either way. Database changes run at start and cannot be
undone by an older image, so take a backup before you update:

```bash
docker compose exec neorouter neorouter backup
```
