#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VAR="$ROOT/var/dev"
for name in temporal opa nats; do
  if [[ -f "$VAR/$name.pid" ]]; then
    pid="$(cat "$VAR/$name.pid")"
    if kill -0 "$pid" 2>/dev/null; then echo "stopping $name ($pid)"; kill "$pid" || true; fi
    rm -f "$VAR/$name.pid"
  fi
done
