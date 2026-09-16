#!/bin/sh
# Materialize the ledger and the ingress as macOS user agents.
#
# This is process supervision and nothing else. launchd keeps two processes
# present; it makes no statement about temporal correctness, and it is not the
# independent Heartime watchdog. Both processes recover from their own durable
# state, so a restart is an ordinary restart.
#
#   ops/launchd/install.sh /Users/you/powerfarm-unattended [http://127.0.0.1:8810/]
set -e
PREFIX="${1:?usage: install.sh PREFIX [ENDPOINT]}"
ENDPOINT="${2:-http://127.0.0.1:8810/}"
AGENTS="$HOME/Library/LaunchAgents"
HERE="$(cd "$(dirname "$0")" && pwd)"
mkdir -p "$AGENTS"
for label in heartime ingress; do
  plist="$AGENTS/work.minilab.powerfarm.$label.plist"
  sed -e "s|@PREFIX@|$PREFIX|g" -e "s|@ENDPOINT@|$ENDPOINT|g" \
      "$HERE/work.minilab.powerfarm.$label.plist" > "$plist"
  # Replacing a loaded agent: remove it first, then bootstrap the new definition.
  launchctl bootout "gui/$(id -u)/work.minilab.powerfarm.$label" 2>/dev/null || true
  launchctl bootstrap "gui/$(id -u)" "$plist"
  launchctl enable "gui/$(id -u)/work.minilab.powerfarm.$label"
  echo "loaded work.minilab.powerfarm.$label"
done
