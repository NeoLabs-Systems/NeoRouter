# syntax=docker/dockerfile:1.7
#
# NeoRouter container image.
#
#   docker build -t neorouter .                                  # stable channel
#   docker build --build-arg NEOROUTER_CHANNEL=beta -t neorouter:beta .
#
# The dashboard (flutter_app/build/web) travels with the checkout: every release
# builds and commits it, so the image never downloads it. Runtime state (SQLite
# database, secret.key, downloaded models, backups) lives in /data; everything
# else in the image is read-only to the process.

FROM golang:1.27-trixie AS go-builder
ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0 GOWORK=off
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w" -o /out/neorouter ./cmd/neorouter

# The stage names `runtime` and `runtime-from-source` are the build targets
# existing compose files ask for.
FROM debian:trixie-slim AS runtime

LABEL org.opencontainers.image.title="NeoRouter" \
      org.opencontainers.image.description="Self-hosted inference router: one endpoint for every model" \
      org.opencontainers.image.url="https://github.com/NeoLabs-Systems/NeoRouter" \
      org.opencontainers.image.source="https://github.com/NeoLabs-Systems/NeoRouter" \
      org.opencontainers.image.licenses="AGPL-3.0-only"

# Which NeoRouter release the image represents: the default release channel
# inside it (`neorouter channel` / `update`), so beta and stable installs are one
# build argument apart.
ARG NEOROUTER_CHANNEL=stable

# ffmpeg is optional for NeoRouter (speech output is converted to the format the
# client asked for), libgomp1 is what llama.cpp needs for local models, and tini
# is PID 1 so SIGTERM reaches the server and the scheduler shuts down instead of
# the container timing out.
#
# The process runs as an unprivileged uid; the data volume is the only thing it
# may write. uid 1000 is the default because a bind-mounted data directory then
# only needs chown to this uid.
ARG UID=1000
ARG GID=1000
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates ffmpeg libgomp1 tini \
 && rm -rf /var/lib/apt/lists/* \
 && if ! getent group "$GID" >/dev/null; then groupadd --gid "$GID" neorouter; fi \
 && if ! getent passwd "$UID" >/dev/null; then \
      useradd --uid "$UID" --gid "$GID" --home-dir /data --no-create-home --shell /usr/sbin/nologin neorouter; \
    fi \
 && mkdir -p /data \
 && chown "$UID:$GID" /data \
 && chmod 0700 /data

WORKDIR /app

COPY --from=go-builder /out/neorouter /usr/local/bin/neorouter
COPY LICENSE NOTICE ./
# The documented defaults (`neorouter install` seeds an install's .env from it).
COPY .env.example ./
COPY landing ./landing
COPY flutter_app/build/web ./flutter_app/build/web
COPY docker/env.example ./docker/env.example
COPY docker/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
COPY docker/node-healthcheck.sh /usr/local/bin/node
RUN chmod 0755 /usr/local/bin/docker-entrypoint.sh /usr/local/bin/node

# NEOROUTER_HOME is the runtime data directory, NEOROUTER_APP_DIR the directory
# the server reads landing/ and flutter_app/build/web from. NEOROUTER_CONTAINER
# makes the CLI behave as a container deployment (no service to install).
#
# NEOROUTER_HOST: the container's only listener must be reachable from outside
# it; the default 127.0.0.1 would make the published port answer nothing.
ENV NEOROUTER_HOME=/data \
    NEOROUTER_APP_DIR=/app \
    NEOROUTER_CONTAINER=1 \
    NEOROUTER_HOST=0.0.0.0 \
    NEOROUTER_PORT=4600 \
    NEOROUTER_RELEASE_CHANNEL=${NEOROUTER_CHANNEL}

VOLUME ["/data"]
EXPOSE 4600
USER ${UID}:${GID}

HEALTHCHECK --interval=30s --timeout=5s --start-period=60s --retries=3 \
  CMD ["neorouter", "healthcheck"]

ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/docker-entrypoint.sh"]
CMD ["serve"]

FROM runtime AS runtime-from-source
