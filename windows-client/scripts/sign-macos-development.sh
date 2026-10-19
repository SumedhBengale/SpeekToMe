#!/bin/sh
# Local testing only. Releases must use Developer ID signing and notarization.
set -eu
app=$1
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
# Without an explicit requirement, ad-hoc signing identifies each build by its
# changing cdhash. TCC then rejects grants made to the previous build. This
# identifier-only requirement is intentionally limited to local test bundles;
# it does not authenticate a publisher and must never be used for distribution.
codesign --force --sign - --options runtime \
  --requirements '=designated => identifier "me.speekto.SpeekToMe"' \
  --entitlements "$script_dir/../src-tauri/Entitlements.plist" "$app"
codesign --verify --strict "$app"
