# syntax=docker/dockerfile:1.7
#
# NeoRouter container image.
#
#   docker build -t neorouter .                                   # prebuilt web client
#   docker build --target runtime-from-source -t neorouter:src .   # build the UI with Flutter
#
# Runtime state (SQLite database, secret.key, downloaded models, backups) lives
# in /data. Everything else in the image is read-only to the process.

ARG NODE_VERSION=22
ARG FLUTTER_VERSION=3.35.5

# ---------------------------------------------------------------------------
# Dependencies: production node_modules, native modules built for this image.
# ---------------------------------------------------------------------------
FROM node:${NODE_VERSION}-bookworm-slim AS deps

ENV NPM_CONFIG_UPDATE_NOTIFIER=false \
    NPM_CONFIG_FUND=false \
    NPM_CONFIG_AUDIT=false

# better-sqlite3 and bcrypt are native: always have the toolchain here, even
# when a prebuilt binary happens to be available for the target platform.
RUN apt-get update \
 && apt-get install -y --no-install-recommends python3 make g++ ca-certificates \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY package.json package-lock.json ./

# npm ci installs exactly the lockfile. The install scripts npm skips for
# packages it does not trust (package.json allowScripts) are run by the
# explicit rebuild below, and the result is verified: a module without its
# binary only fails later, at runtime, inside a container.
RUN --mount=type=cache,target=/root/.npm \
    npm ci --omit=dev --no-audit --no-fund \
 && (node -e "new (require('better-sqlite3'))(':memory:').close(); require('bcrypt')" \
     || npm rebuild better-sqlite3 bcrypt) \
 && node -e "new (require('better-sqlite3'))(':memory:').close(); require('bcrypt')"

# ---------------------------------------------------------------------------
# Web client, prebuilt: the tarball GitHub attaches to the rolling web-main /
# web-beta releases (.github/workflows/web-client.yml). This is what an npm
# install without the Flutter SDK uses as well.
# ---------------------------------------------------------------------------
FROM debian:bookworm-slim AS web-prebuilt

ARG NEOROUTER_WEB_CHANNEL=main
# Point this at a local mirror or file:// path for air-gapped builds.
ARG NEOROUTER_WEB_URL=

RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /web
RUN set -eux; \
    channel="${NEOROUTER_WEB_CHANNEL}"; \
    case "$channel" in \
      beta) release=web-beta ;; \
      main|stable) release=web-main ;; \
      *) echo "NEOROUTER_WEB_CHANNEL must be main or beta, got '$channel'" >&2; exit 1 ;; \
    esac; \
    url="${NEOROUTER_WEB_URL:-https://github.com/NeoLabs-Systems/NeoRouter/releases/download/${release}/neorouter-web.tar.gz}"; \
    echo "Downloading web client: $url"; \
    curl -fsSL --retry 3 --retry-delay 2 -o web.tar.gz "$url"; \
    tar -xzf web.tar.gz; \
    rm web.tar.gz; \
    test -f index.html; \
    if [ ! -f main.dart.js ] && [ ! -f flutter_bootstrap.js ] && [ ! -f flutter.js ]; then \
      echo "The web client archive does not contain a Flutter build." >&2; exit 1; \
    fi

# ---------------------------------------------------------------------------
# Web client, from source: for a fork that changed the Flutter app and has no
# release to download from. Slow (the SDK image is large) and only built when
# the runtime-from-source target is requested.
# ---------------------------------------------------------------------------
FROM ghcr.io/cirruslabs/flutter:${FLUTTER_VERSION} AS web-source

WORKDIR /app/flutter_app
COPY flutter_app/pubspec.yaml flutter_app/pubspec.lock flutter_app/l10n.yaml ./
COPY flutter_app ./

# The full icon font keeps its glyphs stable across releases, so a cached font
# from an older build never shows blank icons.
RUN flutter pub get \
 && flutter build web --release --no-tree-shake-icons --base-href /app/ \
 && test -f build/web/index.html

# ---------------------------------------------------------------------------
# Runtime
# ---------------------------------------------------------------------------
FROM node:${NODE_VERSION}-bookworm-slim AS runtime

LABEL org.opencontainers.image.title="NeoRouter" \
      org.opencontainers.image.description="Self-hosted inference router: one endpoint for every model" \
      org.opencontainers.image.url="https://github.com/NeoLabs-Systems/NeoRouter" \
      org.opencontainers.image.source="https://github.com/NeoLabs-Systems/NeoRouter" \
      org.opencontainers.image.licenses="AGPL-3.0-only"

# ffmpeg is optional for NeoRouter (speech output is converted to the format
# the client asked for), tini is PID 1 so SIGTERM reaches the server and the
# scheduler shuts down instead of the container timing out.
#
# The process runs as an unprivileged uid; the data volume is the only thing it
# may write. node:*-slim already owns uid 1000 (the `node` user), so the user is
# created only when the requested uid is free — a bind-mounted data directory
# then needs chown to this uid.
ARG UID=1000
ARG GID=1000
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates ffmpeg tini \
 && rm -rf /var/lib/apt/lists/* \
 && if ! getent group "$GID" >/dev/null; then groupadd --gid "$GID" neorouter; fi \
 && if ! getent passwd "$UID" >/dev/null; then \
      useradd --uid "$UID" --gid "$GID" --create-home --shell /usr/sbin/nologin neorouter; \
    fi

WORKDIR /app

COPY --chown=${UID}:${GID} . .
COPY --from=deps --chown=${UID}:${GID} /app/node_modules ./node_modules
COPY --from=web-prebuilt --chown=${UID}:${GID} /web ./flutter_app/build/web

COPY docker/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
# The symlink makes `docker compose exec neorouter neorouter status` work
# without a global npm install.
RUN chmod 0755 /usr/local/bin/docker-entrypoint.sh \
 && chmod 0755 /app/bin/neorouter.js \
 && ln -sf /app/bin/neorouter.js /usr/local/bin/neorouter \
 && mkdir -p /data \
 && chown ${UID}:${GID} /data \
 && chmod 0700 /data

ENV NODE_ENV=production \
    NEOROUTER_HOME=/data \
    # The container's only listener must be reachable from outside it; the
    # default 127.0.0.1 would make the published port answer nothing.
    NEOROUTER_HOST=0.0.0.0 \
    NEOROUTER_PORT=4600

VOLUME ["/data"]
EXPOSE 4600
USER ${UID}:${GID}

HEALTHCHECK --interval=30s --timeout=5s --start-period=40s --retries=3 \
  CMD ["node", "-e", "fetch('http://127.0.0.1:' + (process.env.NEOROUTER_PORT || 4600) + '/health').then((r) => process.exit(r.ok ? 0 : 1)).catch(() => process.exit(1))"]

ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/docker-entrypoint.sh"]
CMD ["serve"]

# ---------------------------------------------------------------------------
# Same runtime, UI built from this checkout instead of downloaded.
# ---------------------------------------------------------------------------
FROM runtime AS runtime-from-source

ARG UID=1000
ARG GID=1000
COPY --from=web-source --chown=${UID}:${GID} /app/flutter_app/build/web ./flutter_app/build/web
