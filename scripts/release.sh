#!/bin/bash
# Publishes a Quarters release from this Mac: the signed, notarized app on
# GitHub and in Homebrew (amithgc/tap), with the Linux servers.
#
#   scripts/release.sh 0.5.0             # the whole release
#   scripts/release.sh 0.5.0 --dry-run   # everything up to the build; nothing is pushed
#
# Before the first release on a Mac (once):
#   - a "Developer ID Application" certificate in the login keychain
#     (Xcode → Settings → Accounts → Manage Certificates → + )
#   - the notary login saved as a keychain profile:
#       xcrun notarytool store-credentials quarters-notary --apple-id YOU --team-id TEAMID
#   - gh signed in to an account that can push to amithgc/quarters and
#     amithgc/homebrew-tap
#
# Each release:
#   1. Write the changes under "## Unreleased" (or "## VERSION") in CHANGELOG.md.
#   2. Run this script on main with everything committed.
#
# What it does: checks all of that, sets the version (wails.json and the
# changelog heading), runs the lint and tests, builds, signs and notarizes
# the app (scripts/package-mac.sh), builds the Linux servers, commits
# "Release VERSION", tags vVERSION, pushes both, publishes the GitHub
# release with the changelog section as its notes, and updates the cask and
# formula in the tap (scripts/tap-files.sh). The release workflow skips a
# version that is already released, so tagging here doesn't build it twice.
set -euo pipefail

VERSION="${1:?usage: scripts/release.sh VERSION [--dry-run]}"
VERSION="${VERSION#v}"
DRY=false
[[ "${2:-}" == "--dry-run" ]] && DRY=true
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "VERSION must look like 1.2.3"; exit 1; }

REPO=amithgc/quarters
TAP_REPO=amithgc/homebrew-tap
TAG="v$VERSION"
NOTARY_PROFILE="${NOTARY_PROFILE:-quarters-notary}"

cd "$(dirname "$0")/.."
step() { printf '\n\033[1;33m==> %s\033[0m\n' "$*"; }
fail() { printf '\033[1;31mrelease: %s\033[0m\n' "$*" >&2; exit 1; }

# ---- checks: nothing is changed until all of these pass ----
step "checking $TAG"
command -v gh >/dev/null || fail "gh is not installed (brew install gh)"
gh auth status >/dev/null 2>&1 || fail "gh is not signed in (gh auth login)"
[[ "$(git branch --show-current)" == main ]] || fail "not on main"
[[ -z "$(git status --porcelain --untracked-files=no -- . ':!CHANGELOG.md')" ]] || fail "commit your changes first (only CHANGELOG.md may be uncommitted)"
git fetch -q origin main --tags
[[ "$(git rev-parse HEAD)" == "$(git rev-parse origin/main)" ]] || fail "main is not the same as origin/main (pull or push first)"
git rev-parse -q --verify "refs/tags/$TAG" >/dev/null && fail "$TAG already exists"
gh release view "$TAG" -R "$REPO" >/dev/null 2>&1 && fail "$TAG is already released"
IDENTITY="${MAC_SIGN_IDENTITY:-$(security find-identity -v -p codesigning | grep -o '"Developer ID Application: [^"]*"' | head -1 | tr -d '"')}"
[[ -n "$IDENTITY" ]] || fail "no Developer ID Application certificate in the keychain"
xcrun notarytool history --keychain-profile "$NOTARY_PROFILE" >/dev/null 2>&1 \
  || fail "no notary profile \"$NOTARY_PROFILE\" (xcrun notarytool store-credentials $NOTARY_PROFILE …)"
if ! grep -qE "^## $VERSION( |$)" CHANGELOG.md; then
  grep -q '^## Unreleased' CHANGELOG.md || fail "CHANGELOG.md has neither \"## Unreleased\" nor \"## $VERSION\""
fi
echo "signing as: $IDENTITY"

# ---- the version ----
step "setting the version to $VERSION"
python3 - "$VERSION" <<'EOF'
import re, sys
v = sys.argv[1]
# only the version line changes, so wails.json keeps its layout
w = open("wails.json").read()
w, n = re.subn(r'("productVersion":\s*")[^"]*(")', rf"\g<1>{v}\g<2>", w)
assert n == 1, "productVersion not found in wails.json"
open("wails.json", "w").write(w)
s = open("CHANGELOG.md").read()
if not re.search(rf"^## {re.escape(v)}( |$)", s, re.M):
    s = s.replace("## Unreleased", f"## {v}", 1)
open("CHANGELOG.md", "w").write(s)
EOF
# the release notes: this version's section of the changelog
NOTES="$(mktemp)"
awk -v h="## $VERSION" 'index($0, h) == 1 && (length($0) == length(h) || substr($0, length(h) + 1, 1) == " ") {on = 1; next} on && /^## / {exit} on' CHANGELOG.md > "$NOTES"
[[ -s "$NOTES" ]] || fail "the changelog section for $VERSION is empty"

# ---- checks on the code ----
step "lint and tests"
make lint
make test

# ---- the builds ----
step "the Mac app: build, sign, notarize"
NOTARY_PROFILE="$NOTARY_PROFILE" MAC_SIGN_IDENTITY="$IDENTITY" WAILS="${WAILS:-$(command -v wails || echo "$HOME/go/bin/wails")}" \
  scripts/package-mac.sh "$VERSION"
ZIP="build/release/Quarters-$VERSION.zip"
MAC_SHA="$(cat "$ZIP.sha256")"

step "the Linux servers"
make server
for arch in amd64 arm64; do
  d="$(mktemp -d)"
  cp "build/bin/quarters-linux-$arch" "$d/quarters"
  tar -czf "build/release/quarters-linux-$arch.tar.gz" -C "$d" quarters
  rm -rf "$d"
done
AMD_SHA="$(shasum -a 256 build/release/quarters-linux-amd64.tar.gz | cut -d' ' -f1)"
ARM_SHA="$(shasum -a 256 build/release/quarters-linux-arm64.tar.gz | cut -d' ' -f1)"

if $DRY; then
  step "dry run: nothing committed or pushed"
  echo "built: $ZIP (sha256 $MAC_SHA)"
  echo "       build/release/quarters-linux-{amd64,arm64}.tar.gz"
  echo "notes:"; cat "$NOTES"
  echo
  echo "wails.json and CHANGELOG.md now say $VERSION; undo with: git checkout wails.json CHANGELOG.md"
  exit 0
fi

# ---- publish ----
step "commit, tag and push $TAG"
git add wails.json CHANGELOG.md
git commit -q -m "Release $VERSION"
git tag -a "$TAG" -m "Quarters $VERSION"
git push -q origin main "$TAG"

step "the GitHub release"
gh release create "$TAG" -R "$REPO" --title "Quarters $VERSION" --notes-file "$NOTES" \
  "$ZIP" build/release/quarters-linux-amd64.tar.gz build/release/quarters-linux-arm64.tar.gz

step "the Homebrew tap"
TAP="$(mktemp -d)"
gh repo clone "$TAP_REPO" "$TAP" -- -q
scripts/tap-files.sh "$VERSION" "$MAC_SHA" "$AMD_SHA" "$ARM_SHA" "$TAP"
git -C "$TAP" add -A
if git -C "$TAP" diff --cached --quiet; then
  echo "the tap is already up to date"
else
  git -C "$TAP" -c user.name="$(git config user.name)" -c user.email="$(git config user.email)" commit -q -m "Quarters $VERSION"
  git -C "$TAP" push -q
fi
rm -rf "$TAP" "$NOTES"

step "released $TAG"
echo "https://github.com/$REPO/releases/tag/$TAG"
echo "brew update && brew install --cask amithgc/tap/quarters    # or: brew upgrade --cask quarters"
