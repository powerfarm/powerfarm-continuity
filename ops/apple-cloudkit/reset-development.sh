#!/bin/zsh
set -euo pipefail

TEAM_ID="${POWERFARM_APPLE_TEAM_ID:-WJ9D9WL6VC}"
CONTAINER_ID="${POWERFARM_CLOUDKIT_CONTAINER:-iCloud.app.powerfarm}"
RECEIPT_DIR="${POWERFARM_RECEIPT_DIR:-$HOME/POWERFARM/.receipts/cloudkit-reset-$(date +%Y%m%dT%H%M%S)}"

mkdir -p "$RECEIPT_DIR"
chmod 700 "$RECEIPT_DIR"

if ! security find-generic-password -s com.apple.icloud.cktool -a cktoolmanagement_auth >/dev/null 2>&1; then
  echo "cloudkit_management_token=missing" >&2
  echo "action=xcrun cktool save-token --type management" >&2
  exit 78
fi

xcrun cktool export-schema \
  --team-id "$TEAM_ID" \
  --container-id "$CONTAINER_ID" \
  --environment production \
  --output-file "$RECEIPT_DIR/production.before.ckdb"

xcrun cktool export-schema \
  --team-id "$TEAM_ID" \
  --container-id "$CONTAINER_ID" \
  --environment development \
  --output-file "$RECEIPT_DIR/development.before.ckdb"

xcrun cktool reset-schema \
  --team-id "$TEAM_ID" \
  --container-id "$CONTAINER_ID"

xcrun cktool export-schema \
  --team-id "$TEAM_ID" \
  --container-id "$CONTAINER_ID" \
  --environment development \
  --output-file "$RECEIPT_DIR/development.after.ckdb"

(
  cd "$RECEIPT_DIR"
  shasum -a 256 production.before.ckdb development.before.ckdb development.after.ckdb > SHA256SUMS
)

chmod 600 "$RECEIPT_DIR"/*
echo "result=reset-development-to-production"
echo "container=$CONTAINER_ID"
echo "receipt_dir=$RECEIPT_DIR"
