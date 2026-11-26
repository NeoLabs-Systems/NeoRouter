#!/bin/sh
# Hands a Node.js installation of NeoRouter over to the Go binary.
#
# NeoRouter's Node.js updater (`neorouter update`, or Update in the admin UI)
# checks out the new release and runs `npm install` in it. package.json in this
# tree exists only so that npm runs this script; .npmrc makes that install a dry
# run, so the old node_modules stay intact for the updater's rollback. A global
# install runs `npm install -g` from GitHub instead, which runs this script
# while preparing the download: the package is named differently from the old
# one so that this never touches the installed Node.js version.
# The updater cannot run this release, so this script:
#   1. copies install.sh and itself out of the checkout (the rollback resets it),
#   2. starts the handover in the background, waiting for the updater to exit,
#   3. fails the npm install, so the updater rolls back at once and keeps the
#      Node.js version running meanwhile.
# The handover then runs install.sh, which installs the Go binary and runs
# `neorouter install`: it downloads the app, stops the Node.js server, backs up
# the data, points the launchd/systemd service at the binary and restores
# everything if the new server does not answer.
set -u

if [ "${1:-}" = "--after" ]; then
  updater="$2"; installer="$3"; channel="$4"
  while kill -0 "$updater" 2>/dev/null; do sleep 2; done
  sleep 3
  echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) handing over to the Go binary (channel ${channel})"
  # install.sh replaces the Node.js `neorouter` command before the new server
  # has proven itself, so keep what was there.
  launcher="${NEOROUTER_BIN_DIR:-$HOME/.local/bin}/neorouter"
  saved=""
  if [ -f "$launcher" ] && [ ! -L "$launcher" ]; then
    saved="${installer}.launcher"
    cp -p "$launcher" "$saved"
  fi
  link="$(command -v neorouter 2>/dev/null || true)"
  target=""
  if [ -n "$link" ] && [ -L "$link" ]; then target="$(readlink "$link")"; fi
  # The old server's checkout must not be served by the new one.
  unset NEOROUTER_APP_DIR
  NEOROUTER_RELEASE_CHANNEL="$channel" bash "$installer" </dev/null
  status=$?
  if [ "$status" -ne 0 ]; then
    if [ -n "$saved" ]; then cp -p "$saved" "$launcher"; fi
    if [ -n "$target" ] && [ ! -e "$link" ] && [ ! -L "$link" ]; then ln -s "$target" "$link"; fi
    echo "The switch did not finish; NeoRouter keeps running on Node.js. Run install.sh to try again."
  fi
  rm -f "$installer" "${installer}.launcher" "$0"
  exit "$status"
fi

# The updater is the Node.js CLI or the admin UI's update runner among this
# script's ancestors (npm and its shells sit in between).
updater=""
pid=$$
for _ in 1 2 3 4 5 6 7 8; do
  pid="$(ps -o ppid= -p "$pid" 2>/dev/null | tr -d ' ')"
  [ -n "$pid" ] && [ "$pid" != "1" ] || break
  case "$(ps -o command= -p "$pid" 2>/dev/null)" in
    # Run as bin/neorouter.js, through the npm link (".../bin/neorouter update"),
    # or as the admin UI's update runner.
    *bin/neorouter.js*|*/neorouter\ update*|*update_runner.js*) updater="$pid"; break ;;
  esac
done
if [ -z "$updater" ]; then
  echo "NeoRouter no longer uses npm. Install or update it with install.sh: https://github.com/NeoLabs-Systems/NeoRouter" >&2
  exit 1
fi

root="$(cd "$(dirname "$0")/.." && pwd)"
home="${NEOROUTER_HOME:-$HOME/.neorouter}"
logs="$home/logs"
mkdir -p "$logs"
# The channel the Node.js install followed: its setting, else its branch, else
# the Node.js default.
channel="$(sed -n 's/^NEOROUTER_RELEASE_CHANNEL=//p' "$home/.env" 2>/dev/null | head -n 1 | tr -d ' \r' | tr '[:upper:]' '[:lower:]')"
if [ -z "$channel" ]; then
  case "$(git -C "$root" rev-parse --abbrev-ref HEAD 2>/dev/null)" in main) channel="stable" ;; *) channel="beta" ;; esac
fi
case "$channel" in beta|preview|prerelease|pre-release) channel="beta" ;; *) channel="stable" ;; esac

work="$(mktemp -d "${TMPDIR:-/tmp}/neorouter-handover.XXXXXX")"
cp "$root/install.sh" "$work/install.sh"
cp "$0" "$work/node-handover.sh"
nohup sh "$work/node-handover.sh" --after "$updater" "$work/install.sh" "$channel" >>"$logs/handover.log" 2>&1 &

echo "NeoRouter now runs as a single program instead of Node.js. This update rolls back first and keeps NeoRouter running; the switch then finishes by itself in the background (progress: $logs/handover.log)." >&2
exit 1
