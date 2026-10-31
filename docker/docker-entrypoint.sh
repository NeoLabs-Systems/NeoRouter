#!/bin/sh
# Arguments are NeoRouter CLI commands (`serve` by default):
#
#   docker run … neorouter doctor
#   docker compose exec neorouter reset-password admin 'a-long-password'
#
# `sh`, `bash` and `node` are run as given, for debugging.
set -eu

if [ "$#" -eq 0 ]; then
  set -- serve
fi

case "$1" in
  sh|bash|/bin/sh|/bin/bash|node)
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

# `neorouter serve` copies .env.example into the runtime home when there is no
# configuration yet, and that example binds 127.0.0.1 — which inside a
# container means the published port answers nothing. Seed the file with the
# container defaults first so the first start is usable as-is.
if [ ! -f "$home/.env" ] && [ -f /app/docker/env.default ]; then
  cp /app/docker/env.default "$home/.env"
  chmod 0600 "$home/.env"
fi

exec node /app/bin/neorouter.js "$@"
