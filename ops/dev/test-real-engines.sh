#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
./ops/dev/start-local.sh
cleanup() { ./ops/dev/stop-local.sh >/dev/null 2>&1 || true; }
trap cleanup EXIT

wait_http() {
  local url="$1"
  for _ in $(seq 1 60); do curl -fsS "$url" >/dev/null 2>&1 && return 0; sleep .25; done
  return 1
}
wait_tcp() {
  local host="$1" port="$2"
  for _ in $(seq 1 60); do (echo >"/dev/tcp/$host/$port") >/dev/null 2>&1 && return 0; sleep .25; done
  return 1
}

wait_http http://127.0.0.1:8181/health || { tail -50 var/dev/opa.log; exit 1; }
wait_http http://127.0.0.1:8222/varz || { tail -50 var/dev/nats.log; exit 1; }
wait_tcp 127.0.0.1 7233 || { tail -80 var/dev/temporal.log; exit 1; }

go test ./...
go run ./cmd/continuity smoke
