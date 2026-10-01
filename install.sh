#!/bin/sh
# Installs the rivet binary from a GitHub release.
#
#   curl -fsSL https://raw.githubusercontent.com/wetsocksnsleeves/rivet/main/install.sh | sh
#
# RIVET_VERSION picks a release tag (default: the latest release).
# RIVET_INSTALL_DIR picks the install directory (default: ~/.local/bin).
set -eu

repo="wetsocksnsleeves/rivet"
version="${RIVET_VERSION:-latest}"
install_dir="${RIVET_INSTALL_DIR:-$HOME/.local/bin}"

fail() {
	echo "rivet: $*" >&2
	exit 1
}

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
darwin | linux) ;;
*) fail "unsupported OS: $os" ;;
esac

arch=$(uname -m)
case "$arch" in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) fail "unsupported architecture: $arch" ;;
esac

asset="rivet_${os}_${arch}"
if [ "$version" = latest ]; then
	url="https://github.com/$repo/releases/latest/download/$asset"
else
	url="https://github.com/$repo/releases/download/$version/$asset"
fi

tmp=$(mktemp "${TMPDIR:-/tmp}/rivet.XXXXXX")
trap 'rm -f "$tmp"' EXIT

echo "Downloading $url"
curl -fsSL "$url" -o "$tmp" || fail "download failed: $url"
chmod +x "$tmp"

mkdir -p "$install_dir"
mv "$tmp" "$install_dir/rivet"
echo "Installed rivet to $install_dir/rivet"

case ":$PATH:" in
*":$install_dir:"*) ;;
*) echo "Add $install_dir to your PATH to run rivet." ;;
esac
