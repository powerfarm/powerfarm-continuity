#!/usr/bin/env bash
set -euo pipefail
if [[ $# -ne 1 ]]; then echo "usage: $0 continuity-runtime-binaries.tar.gz" >&2; exit 2; fi
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ARCHIVE="$1"
[[ -f "$ARCHIVE" ]] || { echo "archive not found: $ARCHIVE" >&2; exit 1; }
tar -tzf "$ARCHIVE" | grep -Eq '^runtime/(bin/(opa|nats-server|temporal|cue)|PLATFORM)$' || { echo "archive does not look like a Continuity runtime bundle" >&2; exit 1; }
tar -xzf "$ARCHIVE" -C "$ROOT"
for b in opa nats-server temporal cue; do chmod +x "$ROOT/runtime/bin/$b"; done
echo "installed runtime bundle into $ROOT/runtime"
