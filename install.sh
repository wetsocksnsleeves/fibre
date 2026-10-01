#!/bin/sh
# Installs the fibre binary from a GitHub release.
#
#   curl -fsSL https://raw.githubusercontent.com/wetsocksnsleeves/fibre/main/install.sh | sh
#
# FIBRE_VERSION picks a release tag (default: the latest release).
# FIBRE_INSTALL_DIR picks the install directory (default: ~/.local/bin).
set -eu

repo="wetsocksnsleeves/fibre"
version="${FIBRE_VERSION:-latest}"
install_dir="${FIBRE_INSTALL_DIR:-$HOME/.local/bin}"

fail() {
	echo "fibre: $*" >&2
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

asset="fibre_${os}_${arch}"
if [ "$version" = latest ]; then
	url="https://github.com/$repo/releases/latest/download/$asset"
else
	url="https://github.com/$repo/releases/download/$version/$asset"
fi

tmp=$(mktemp "${TMPDIR:-/tmp}/fibre.XXXXXX")
trap 'rm -f "$tmp"' EXIT

echo "Downloading $url"
curl -fsSL "$url" -o "$tmp" || fail "download failed: $url"
chmod +x "$tmp"

mkdir -p "$install_dir"
mv "$tmp" "$install_dir/fibre"
echo "Installed fibre to $install_dir/fibre"

case ":$PATH:" in
*":$install_dir:"*) ;;
*) echo "Add $install_dir to your PATH to run fibre." ;;
esac
