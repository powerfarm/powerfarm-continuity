#!/bin/zsh
set -euo pipefail

echo "CloudKit authentication bootstrap"
echo "Tokens are saved to the login Keychain by cktool; this script never prints them."
echo
xcrun cktool save-token --type management
xcrun cktool save-token --type user
echo
echo "management_token=stored_in_keychain"
echo "user_token=stored_in_keychain"
echo "note=user tokens are short-lived and require periodic interactive renewal"
