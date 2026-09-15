#!/usr/bin/env bash
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SRC="$HERE/downloaded"
OUT="${1:-$HERE/continuity-engines.tar.gz}"
if [[ ! -d "$SRC" ]]; then
  echo "Nothing downloaded yet. Run ./fetch-engines.sh first." >&2
  exit 1
fi

tar -czf "$OUT" -C "$HERE" downloaded engines.lock.tsv fetch-engines.sh
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum "$OUT" > "$OUT.sha256"
else
  shasum -a 256 "$OUT" > "$OUT.sha256"
fi
printf 'Created %s\n' "$OUT"
printf 'Checksum %s.sha256\n' "$OUT"
