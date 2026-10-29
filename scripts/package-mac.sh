#!/bin/bash
# Builds Quarters.app for Apple silicon and Intel, signs it with a Developer
# ID, has Apple notarize it, and zips it for a release and Homebrew:
#
#   scripts/package-mac.sh 1.2.0
#
# Needs (in the environment):
#   MAC_SIGN_IDENTITY   e.g. "Developer ID Application: Your Name (TEAMID)"
# and, to notarize, either
#   NOTARY_PROFILE      a profile saved with `xcrun notarytool store-credentials`
# or
#   APPLE_ID, APPLE_TEAM_ID, APPLE_APP_PASSWORD   (an app-specific password)
#
# Without notarizing credentials it stops after signing (a local check).
# Output: build/release/Quarters-<version>.zip and its .sha256.
set -euo pipefail

VERSION="${1:?usage: scripts/package-mac.sh VERSION (e.g. 1.2.0)}"
VERSION="${VERSION#v}"
IDENTITY="${MAC_SIGN_IDENTITY:?set MAC_SIGN_IDENTITY to your Developer ID Application identity}"
WAILS="${WAILS:-$(command -v wails || echo "$HOME/go/bin/wails")}"
APP=build/bin/Quarters.app
OUT=build/release
ZIP="$OUT/Quarters-$VERSION.zip"

cd "$(dirname "$0")/.."

# The version shown by macOS (Info.plist) comes from wails.json; set it for
# this build only.
cp wails.json build/wails.json.orig
trap 'mv build/wails.json.orig wails.json' EXIT
python3 - "$VERSION" <<'EOF'
import json, sys
c = json.load(open("wails.json"))
c["info"]["productVersion"] = sys.argv[1]
json.dump(c, open("wails.json", "w"), indent=2)
EOF

echo "==> building $VERSION (universal)"
make vendor >/dev/null
"$WAILS" build -clean -platform darwin/universal -trimpath -ldflags "-s -w"

echo "==> signing as $IDENTITY"
# Hardened runtime and a secure timestamp: both required for notarization.
codesign --force --options runtime --timestamp --sign "$IDENTITY" "$APP"
codesign --verify --strict --verbose=2 "$APP"

mkdir -p "$OUT"
rm -f "$ZIP"
ditto -c -k --keepParent "$APP" "$ZIP"

if [[ -n "${NOTARY_PROFILE:-}" ]]; then
  auth=(--keychain-profile "$NOTARY_PROFILE")
elif [[ -n "${APPLE_ID:-}" ]]; then
  auth=(--apple-id "$APPLE_ID" --team-id "${APPLE_TEAM_ID:?}" --password "${APPLE_APP_PASSWORD:?}")
else
  echo "==> signed only (no notarizing credentials): $ZIP"
  exit 0
fi

echo "==> notarizing (this takes a few minutes)"
xcrun notarytool submit "$ZIP" "${auth[@]}" --wait --timeout 30m | tee "$OUT/notary.log"
grep -q "status: Accepted" "$OUT/notary.log" || { echo "notarization failed; see: xcrun notarytool log <id>"; exit 1; }

# Staple the ticket so the app opens offline too, then zip the stapled app.
xcrun stapler staple "$APP"
spctl --assess --type execute --verbose=2 "$APP"
rm -f "$ZIP"
ditto -c -k --keepParent "$APP" "$ZIP"
shasum -a 256 "$ZIP" | awk '{print $1}' > "$ZIP.sha256"
echo "==> $ZIP (sha256 $(cat "$ZIP.sha256"))"
