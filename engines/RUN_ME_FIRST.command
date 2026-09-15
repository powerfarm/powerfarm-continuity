#!/usr/bin/env bash
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$HERE"
echo "Fetching the full pinned Continuity OSS engine set..."
./fetch-engines.sh all
./pack-engines.sh continuity-engines.tar.gz
echo
echo "DONE"
echo "Bring this file back to ChatGPT:"
echo "  $HERE/continuity-engines.tar.gz"
echo "and optionally its checksum:"
echo "  $HERE/continuity-engines.tar.gz.sha256"
