#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VAR="$ROOT/var/dev"
for name in nats opa temporal; do
  if [[ -f "$VAR/$name.pid" ]] && kill -0 "$(cat "$VAR/$name.pid")" 2>/dev/null; then echo "$name: running pid $(cat "$VAR/$name.pid")"; else echo "$name: stopped"; fi
done
