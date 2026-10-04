#!/bin/sh
# install.sh — installs the gogogo installer + MCP server from the latest
# GitHub release into $BIN_DIR (default ~/.local/bin). Verifies SHA256.
#
#   curl -sSfL https://raw.githubusercontent.com/calionauta/gogogo/master/install.sh | sh
#
# Env: BIN_DIR (install dir), REPO (owner/repo), GOGOGO_VERSION (pin, e.g. v0.32.0).
set -eu

REPO="${REPO:-calionauta/gogogo}"
BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"
VERSION="${GOGOGO_VERSION:-latest}"

OS="$(uname -s)"
ARCH="$(uname -m)"
case "$OS" in
  Linux) OS="Linux" ;;
  Darwin) OS="Darwin" ;;
  *) echo "install.sh: unsupported OS $OS (Linux/Darwin only)" >&2; exit 1 ;;
esac
case "$ARCH" in
  x86_64|amd64) ARCH="x86_64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) echo "install.sh: unsupported arch $ARCH (x86_64/arm64 only)" >&2; exit 1 ;;
esac

# GoReleaser names: gogogo-<Version>-<Os>-<Arch>.tar.gz, checksum SHA256SUMS.txt.
# NOTE: GoReleaser's {{.Os}}/{{.Arch}} render "Linux/x86_64", "Darwin/arm64".
if [ "$VERSION" = "latest" ]; then
  TAG="$(curl -sSfL -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" | sed 's/.*\///')"
else
  TAG="$VERSION"
fi
BASE="https://github.com/$REPO/releases/download/$TAG"
TARBALL="gogogo-$TAG-$OS-$ARCH.tar.gz"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT INT TERM
cd "$TMP"
curl -sSfL -o "$TARBALL" "$BASE/$TARBALL"
curl -sSfL "$BASE/SHA256SUMS.txt" | grep " $TARBALL\$" > SHA256SUMS.want

if command -v sha256sum >/dev/null 2>&1; then
  sha256sum -c SHA256SUMS.want
elif command -v shasum >/dev/null 2>&1; then
  shasum -a 256 -c SHA256SUMS.want
else
  echo "install.sh: need sha256sum or shasum to verify the download" >&2
  exit 1
fi

mkdir -p "$BIN_DIR"
tar -xzf "$TARBALL" -C "$BIN_DIR" gogogo gogogo-mcp
chmod +x "$BIN_DIR/gogogo" "$BIN_DIR/gogogo-mcp"

echo "installed: $BIN_DIR/gogogo $BIN_DIR/gogogo-mcp ($TAG)"
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) echo "note: $BIN_DIR is not on PATH — add: export PATH=\"\$BIN_DIR:\$PATH\"" ;;
esac
"$BIN_DIR/gogogo" --version
