#!/bin/bash
# Writes the Homebrew cask (the Mac app) and formula (the Linux server) for
# a release into a checkout of amithgc/homebrew-tap:
#
#   scripts/tap-files.sh VERSION MAC_ZIP_SHA256 LINUX_AMD64_SHA256 LINUX_ARM64_SHA256 TAP_DIR
#
# Used by scripts/release.sh and .github/workflows/release.yml, so a
# release made from a Mac and one made by GitHub write the same files.
set -euo pipefail

V="${1:?usage: scripts/tap-files.sh VERSION MAC_SHA AMD64_SHA ARM64_SHA TAP_DIR}"
MAC_SHA="${2:?}" AMD_SHA="${3:?}" ARM_SHA="${4:?}" TAP="${5:?}"
V="${V#v}"
for sha in "$MAC_SHA" "$AMD_SHA" "$ARM_SHA"; do
  [[ "$sha" =~ ^[0-9a-f]{64}$ ]] || { echo "not a sha256: $sha" >&2; exit 1; }
done

mkdir -p "$TAP/Casks" "$TAP/Formula"

cat > "$TAP/Casks/quarters.rb" <<EOF
cask "quarters" do
  version "$V"
  sha256 "$MAC_SHA"

  url "https://github.com/amithgc/quarters/releases/download/v#{version}/Quarters-#{version}.zip"
  name "Quarters"
  desc "Minimalist home for terminal coding agents across many projects"
  homepage "https://quarters.amithgc.com/"

  livecheck do
    url :url
    strategy :github_latest
  end

  depends_on :macos

  app "Quarters.app"

  zap trash: [
    "~/.config/quarters",
    "~/Library/Caches/io.github.amithgc.quarters",
    "~/Library/Preferences/io.github.amithgc.quarters.plist",
    "~/Library/WebKit/io.github.amithgc.quarters",
  ]
end
EOF

cat > "$TAP/Formula/quarters-server.rb" <<EOF
class QuartersServer < Formula
  desc "Quarters as a server: coding agents across your projects, from a browser"
  homepage "https://quarters.amithgc.com/"
  version "$V"
  license "MIT"

  depends_on :linux

  on_linux do
    on_intel do
      url "https://github.com/amithgc/quarters/releases/download/v#{version}/quarters-linux-amd64.tar.gz"
      sha256 "$AMD_SHA"
    end
    on_arm do
      url "https://github.com/amithgc/quarters/releases/download/v#{version}/quarters-linux-arm64.tar.gz"
      sha256 "$ARM_SHA"
    end
  end

  def install
    bin.install "quarters"
  end

  test do
    assert_match "set-password", shell_output("#{bin}/quarters --help")
  end
end
EOF
echo "wrote $TAP/Casks/quarters.rb and $TAP/Formula/quarters-server.rb for $V"
