#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
PREFIX="$ROOT/engines/runtime/prefix"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export PKG_CONFIG_PATH="$PREFIX/lib/pkgconfig"
export LD_LIBRARY_PATH="$PREFIX/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
cc "$HERE/server.c" -o "$HERE/server" $(pkg-config --cflags --libs open62541)
cc "$HERE/client.c" -o "$HERE/client" $(pkg-config --cflags --libs open62541)
"$HERE/server" >"$HERE/server.log" 2>&1 &
pid=$!
cleanup() { kill "$pid" >/dev/null 2>&1 || true; wait "$pid" >/dev/null 2>&1 || true; }
trap cleanup EXIT
for _ in $(seq 1 40); do
  if (echo >/dev/tcp/127.0.0.1/4840) >/dev/null 2>&1; then break; fi
  sleep .1
done
"$HERE/client"
