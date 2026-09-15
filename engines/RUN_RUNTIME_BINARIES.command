#!/usr/bin/env bash
set -e
cd "$(dirname "$0")"
./fetch-runtime-binaries.sh
./pack-runtime-binaries.sh
printf '\nBring this file back to ChatGPT:\n  %s\n' "$PWD/continuity-runtime-binaries.tar.gz"
