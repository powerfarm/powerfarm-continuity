#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="$ROOT/runtime/bin"
TMP="$ROOT/runtime/tmp"
mkdir -p "$BIN" "$TMP"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$os" in darwin|linux) ;; *) echo "unsupported OS: $os" >&2; exit 1;; esac
case "$arch" in x86_64|amd64) arch=amd64;; arm64|aarch64) arch=arm64;; *) echo "unsupported arch: $arch" >&2; exit 1;; esac

fetch() {
  local url="$1" out="$2"
  echo "fetch $url"
  curl -fL --retry 3 --retry-delay 1 "$url" -o "$out"
}

# OPA 1.20.2
opa="opa_${os}_${arch}"
fetch "https://openpolicyagent.org/downloads/v1.20.2/${opa}" "$BIN/opa"
chmod +x "$BIN/opa"

# NATS Server 2.14.6
nats_archive="nats-server-v2.14.6-${os}-${arch}.tar.gz"
fetch "https://github.com/nats-io/nats-server/releases/download/v2.14.6/${nats_archive}" "$TMP/$nats_archive"
tar -xzf "$TMP/$nats_archive" -C "$TMP"
cp "$TMP/nats-server-v2.14.6-${os}-${arch}/nats-server" "$BIN/nats-server"
chmod +x "$BIN/nats-server"

# CUE 0.17.1
cue_archive="cue_v0.17.1_${os}_${arch}.tar.gz"
fetch "https://github.com/cue-lang/cue/releases/download/v0.17.1/${cue_archive}" "$TMP/$cue_archive"
tar -xzf "$TMP/$cue_archive" -C "$TMP/cue" --strip-components=0 2>/dev/null || {
  mkdir -p "$TMP/cue"; tar -xzf "$TMP/$cue_archive" -C "$TMP/cue";
}
cue_path="$(find "$TMP/cue" -type f -name cue -perm -111 | head -1 || true)"
if [[ -z "$cue_path" ]]; then cue_path="$(find "$TMP/cue" -type f -name cue | head -1 || true)"; fi
[[ -n "$cue_path" ]] || { echo "could not find cue binary in $cue_archive" >&2; exit 1; }
cp "$cue_path" "$BIN/cue"; chmod +x "$BIN/cue"

# Temporal CLI 1.8.1 is used only for the local development service.
# Production Continuity remains pinned to Temporal Server 1.32.0 source/configuration.
fetch "https://temporal.download/cli/archive/v1.8.1?platform=${os}&arch=${arch}" "$TMP/temporal_cli.tar.gz"
mkdir -p "$TMP/temporal"
tar -xzf "$TMP/temporal_cli.tar.gz" -C "$TMP/temporal"
cp "$TMP/temporal/temporal" "$BIN/temporal"
chmod +x "$BIN/temporal"

printf '\nInstalled runtime binaries:\n'
"$BIN/opa" version | head -3 || true
"$BIN/nats-server" --version || true
"$BIN/cue" version | head -3 || true
"$BIN/temporal" --version || true

cat > "$ROOT/runtime/PLATFORM" <<META
os=$os
arch=$arch
opa=1.20.2
nats-server=2.14.6
cue=0.17.1
temporal-cli=1.8.1
META

echo "runtime binaries ready in $BIN"
