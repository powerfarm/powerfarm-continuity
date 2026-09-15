#!/usr/bin/env bash
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOCK="$HERE/engines.lock.tsv"
OUT="$HERE/downloaded"
PROFILE="${1:-core}"
DRY_RUN=0

if [[ "${1:-}" == "--dry-run" ]]; then
  DRY_RUN=1
  PROFILE="${2:-core}"
fi

case "$PROFILE" in
  core|world|identity|packaging|sandbox|observability|industrial|all) ;;
  *)
    echo "usage: $0 [--dry-run] {core|world|identity|packaging|sandbox|observability|industrial|all}" >&2
    exit 2
    ;;
esac

mkdir -p "$OUT/src" "$OUT/specs" "$OUT/npm" "$OUT/meta" "$OUT/tmp"
FETCHED="$OUT/meta/fetched.tsv"
printf '# name\tprofile\tkind\tversion_or_ref\tresolved\tsha256\n' > "$FETCHED"

sha256_file() {
  local f="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$f" | awk '{print $1}'
  else
    shasum -a 256 "$f" | awk '{print $1}'
  fi
}

want_profile() {
  local p="$1"
  [[ "$PROFILE" == "all" || "$PROFILE" == "$p" || ( "$PROFILE" == "core" && "$p" == "core" ) ]]
}

need() {
  command -v "$1" >/dev/null 2>&1 || { echo "missing required command: $1" >&2; exit 1; }
}

fetch_github_tag() {
  local name="$1" ref="$2" repo="$3"
  local dest="$OUT/src/$name"
  local archive="$OUT/tmp/${name}-${ref//\//_}.tar.gz"
  local url="https://github.com/${repo}/archive/refs/tags/${ref}.tar.gz"

  echo "[github] $name $ref <- $repo"
  if [[ "$DRY_RUN" == 1 ]]; then
    echo "  curl -fL '$url'"
    return
  fi
  need curl; need tar
  rm -rf "$dest" "$archive"
  mkdir -p "$dest"
  curl --fail --location --retry 3 --retry-delay 2 "$url" -o "$archive"
  tar -xzf "$archive" -C "$dest" --strip-components=1
  local sum
  sum="$(sha256_file "$archive")"
  printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$name" "$profile" "github-tag" "$ref" "$repo@$ref" "$sum" >> "$FETCHED"
  rm -f "$archive"
}

fetch_url() {
  local name="$1" ver="$2" url="$3"
  local base
  base="$(basename "${url%%\?*}")"
  local dest="$OUT/specs/${name}-${ver}-${base}"
  echo "[url]    $name $ver <- $url"
  if [[ "$DRY_RUN" == 1 ]]; then
    echo "  curl -fL '$url' -o '$dest'"
    return
  fi
  need curl
  curl --fail --location --retry 3 --retry-delay 2 "$url" -o "$dest"
  local sum
  sum="$(sha256_file "$dest")"
  printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$name" "$profile" "url" "$ver" "$url" "$sum" >> "$FETCHED"
}

fetch_npm() {
  local name="$1" ver="$2" pkg="$3"
  echo "[npm]    $name $ver <- $pkg"
  if [[ "$DRY_RUN" == 1 ]]; then
    echo "  npm pack '${pkg}@${ver}' --pack-destination '$OUT/npm'"
    return
  fi
  need npm
  local before after tarball sum
  before="$(find "$OUT/npm" -maxdepth 1 -type f -name '*.tgz' -print | sort || true)"
  npm pack "${pkg}@${ver}" --pack-destination "$OUT/npm" >/dev/null
  after="$(find "$OUT/npm" -maxdepth 1 -type f -name '*.tgz' -print | sort || true)"
  tarball="$(comm -13 <(printf '%s\n' "$before") <(printf '%s\n' "$after") | tail -n 1)"
  if [[ -z "$tarball" ]]; then
    tarball="$(find "$OUT/npm" -maxdepth 1 -type f -name '*.tgz' -print | sort | tail -n 1)"
  fi
  sum="$(sha256_file "$tarball")"
  printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$name" "$profile" "npm" "$ver" "${pkg}@${ver}" "$sum" >> "$FETCHED"
}

while IFS=$'\t' read -r name profile kind ref source license role; do
  [[ -z "${name:-}" || "$name" == \#* ]] && continue
  want_profile "$profile" || continue
  case "$kind" in
    github-tag) fetch_github_tag "$name" "$ref" "$source" ;;
    url)        fetch_url "$name" "$ref" "$source" ;;
    npm)        fetch_npm "$name" "$ref" "$source" ;;
    *) echo "unknown kind '$kind' for $name" >&2; exit 1 ;;
  esac
done < "$LOCK"

if [[ "$DRY_RUN" == 1 ]]; then
  echo
  echo "Dry run complete. No files downloaded."
  exit 0
fi

cp "$LOCK" "$OUT/meta/engines.lock.tsv"
rm -rf "$OUT/tmp"
echo
echo "Fetched engine set '$PROFILE' into: $OUT"
echo "Resolved manifest: $FETCHED"
