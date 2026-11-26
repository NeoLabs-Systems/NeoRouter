#!/bin/sh
# Installed as `node`: compose files written for the Node.js image check health
# with `node -e "fetch(… /health …)"`. Only that call is answered.
case "$1 $2" in
  "-e "*/health*) exec neorouter healthcheck ;;
esac
echo "neorouter: this image has no Node.js; use 'neorouter healthcheck'." >&2
exit 127
