#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BIN="$ROOT/engines/runtime/bin"
VAR="$ROOT/var/dev"
mkdir -p "$VAR" "$VAR/nats"

need() { [[ -x "$BIN/$1" ]] || { echo "missing $BIN/$1; run engines/RUN_RUNTIME_BINARIES.command" >&2; exit 1; }; }
need nats-server; need opa; need temporal

start_one() {
  local name="$1"; shift
  if [[ -f "$VAR/$name.pid" ]] && kill -0 "$(cat "$VAR/$name.pid")" 2>/dev/null; then
    echo "$name already running pid $(cat "$VAR/$name.pid")"; return
  fi
  echo "starting $name"
  (cd "$ROOT" && nohup "$@" >"$VAR/$name.log" 2>&1 & echo $! >"$VAR/$name.pid")
}

start_one nats "$BIN/nats-server" -js -sd "$VAR/nats" -p 4222 -m 8222
start_one opa "$BIN/opa" run --server --addr 127.0.0.1:8181 "$ROOT/policy/continuity.rego"
start_one temporal "$BIN/temporal" server start-dev --ip 127.0.0.1 --port 7233 --ui-port 8233 --db-filename "$VAR/temporal.db"

echo "started; logs are under $VAR"
