#!/usr/bin/env bash
# NeoRouter standalone installer
# Usage: bash <(curl -fsSL https://raw.githubusercontent.com/NeoLabs-Systems/NeoRouter/main/install.sh)

set -euo pipefail

if [[ -t 1 ]]; then
  BOLD='\033[1m'; RESET='\033[0m'; RED='\033[1;31m'; GRN='\033[1;32m'
  CYN='\033[1;36m'; DIM='\033[2m'
else
  BOLD=''; RESET=''; RED=''; GRN=''; CYN=''; DIM=''
fi

ok()   { echo -e "  ${GRN}✓${RESET}  $*"; }
info() { echo -e "  ${CYN}→${RESET}  $*"; }
err()  { echo -e "  ${RED}✗${RESET}  $*" >&2; }
ask()  {
  local var="$1" prompt="$2" default="${3:-}"
  [[ -n "$default" ]] \
    && echo -ne "  ${CYN}?${RESET}  ${prompt} ${DIM}[${default}]${RESET} " \
    || echo -ne "  ${CYN}?${RESET}  ${prompt} "
  read -r input </dev/tty
  [[ -z "$input" && -n "$default" ]] && input="$default"
  eval "$var=\"\$input\""
}

echo -e "${CYN}${BOLD}NeoRouter installer${RESET}"
echo

MISSING=()
command -v git  &>/dev/null && ok "git $(git --version | awk '{print $3}')"   || MISSING+=("git")
command -v node &>/dev/null && ok "Node.js $(node --version)"                  || MISSING+=("node (https://nodejs.org)")
command -v npm  &>/dev/null && ok "npm $(npm --version)"                       || MISSING+=("npm")

if [[ ${#MISSING[@]} -gt 0 ]]; then
  echo
  err "Missing requirements:"
  for m in "${MISSING[@]}"; do echo "     • $m"; done
  echo
  exit 1
fi

NODE_MAJOR="$(node -p "process.versions.node.split('.')[0]")"
if [[ "$NODE_MAJOR" -lt 20 ]]; then
  err "NeoRouter requires Node.js 20 or newer. Current runtime is $(node --version)."
  exit 1
fi

DEFAULT_DIR="$HOME/NeoRouter"
ask INSTALL_DIR "Install directory" "$DEFAULT_DIR"

if [[ -d "$INSTALL_DIR/.git" ]]; then
  info "Existing repo found at ${INSTALL_DIR} — pulling latest..."
  BRANCH="$(git -C "$INSTALL_DIR" rev-parse --abbrev-ref HEAD)"
  # Files an earlier install run changed itself (npm rewrites the lockfile,
  # Flutter its own lock) are not local edits: put them back first.
  for file in package-lock.json flutter_app/pubspec.lock; do
    git -C "$INSTALL_DIR" checkout -- "$file" 2>/dev/null || true
  done
  # Anything else that changed is kept aside and put back after the update.
  if ! git -C "$INSTALL_DIR" pull --rebase --autostash origin "$BRANCH"; then
    err "Could not update ${INSTALL_DIR}. Your local changes are kept in 'git stash list' there."
    err "To start fresh: rm -rf \"${INSTALL_DIR}\" and run this installer again."
    exit 1
  fi
  ok "Updated"
elif [[ -d "$INSTALL_DIR" && -n "$(ls -A "$INSTALL_DIR" 2>/dev/null)" ]]; then
  err "Directory ${INSTALL_DIR} exists and is not empty. Choose a different path or remove it."
  exit 1
else
  info "Cloning into ${INSTALL_DIR}..."
  git clone https://github.com/NeoLabs-Systems/NeoRouter.git "$INSTALL_DIR"
  ok "Cloned"
fi

echo
info "Installing Node.js dependencies..."
cd "$INSTALL_DIR"
# npm ci installs exactly the lockfile and never rewrites it, so the
# checkout stays clean for the next update.
npm ci --omit=dev --no-audit --no-fund || npm install --omit=dev --no-audit --no-fund
ok "Dependencies ready"

echo
info "Running Node manager installer..."
exec node ./bin/neorouter.js install
