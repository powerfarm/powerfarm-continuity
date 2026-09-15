#!/usr/bin/env bash
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$HERE"
echo "Fetching the minimal Continuity core engine set..."
./fetch-engines.sh core
./pack-engines.sh continuity-core-engines.tar.gz
echo
echo "DONE"
echo "Bring this file back to ChatGPT:"
echo "  $HERE/continuity-core-engines.tar.gz"
