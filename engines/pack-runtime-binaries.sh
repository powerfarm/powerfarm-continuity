#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT="${1:-$ROOT/continuity-runtime-binaries.tar.gz}"
for b in opa nats-server temporal cue; do
  [[ -x "$ROOT/runtime/bin/$b" ]] || { echo "missing runtime/bin/$b" >&2; exit 1; }
done
tar -czf "$OUT" -C "$ROOT" runtime/bin runtime/PLATFORM runtime.lock.tsv
if command -v shasum >/dev/null 2>&1; then shasum -a 256 "$OUT" > "$OUT.sha256"; else sha256sum "$OUT" > "$OUT.sha256"; fi
echo "created $OUT"
echo "created $OUT.sha256"
