#!/bin/sh
# Arguments are NeoRouter CLI commands (`serve` by default):
#
#   docker run … neorouter doctor
#   docker compose exec neorouter reset-password admin 'a-long-password'
#
# `sh` and `bash` are run as given, for debugging.
set -eu

if [ "$#" -eq 0 ]; then
  set -- serve
fi

case "$1" in
  sh|bash|/bin/sh|/bin/bash)
    exec "$@"
    ;;
esac

home="${NEOROUTER_HOME:-/data}"

# The CLI writes the runtime layout it needs; say so plainly instead of dying
# somewhere inside it when the volume is mounted read-only or owned by root.
if ! touch "$home/.write-test" 2>/dev/null; then
  echo "neorouter: $home is not writable by uid $(id -u)." >&2
  echo "neorouter: fix the volume ownership, e.g." >&2
  echo "neorouter:   docker run --rm -v neorouter-data:/data alpine chown -R 1000:1000 /data" >&2
  exit 1
fi
rm -f "$home/.write-test"

# The server reads its configuration from .env in the runtime home. Seed it with
# the container defaults first so the first start is usable as-is; the example
# binds 0.0.0.0, which is what a published port needs.
if [ ! -f "$home/.env" ] && [ -f /app/docker/env.example ]; then
  cp /app/docker/env.example "$home/.env"
  # The image was built for one channel; the file must not claim another.
  if [ -n "${NEOROUTER_RELEASE_CHANNEL:-}" ]; then
    sed -i "s/^NEOROUTER_RELEASE_CHANNEL=.*/NEOROUTER_RELEASE_CHANNEL=${NEOROUTER_RELEASE_CHANNEL}/" "$home/.env"
  fi
  chmod 0600 "$home/.env"
fi

exec neorouter "$@"