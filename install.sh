#!/bin/sh
# install.sh — installs the gogogo installer + MCP server from the latest
# GitHub release into $BIN_DIR (default ~/.local/bin). Verifies SHA256.
# With no Go toolchain on the machine, bootstraps one into ~/.local/go
# (user-space, no sudo, existing installs never touched).
#
#   curl -sSfL https://raw.githubusercontent.com/calionauta/gogogo/master/install.sh | sh
#
# Env: BIN_DIR (install dir), REPO (owner/repo), GOGOGO_VERSION (pin, e.g. v0.32.0),
#      GO_DIR (toolchain dir, default ~/.local/go), SKIP_GO_BOOTSTRAP=1 (offline/tests).
#
# Needs on this machine: sh, curl, tar (+gzip), uname, mktemp, and
# sha256sum or shasum. Checked up front with a precise error.
set -eu

for tool in curl tar uname mktemp; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "install.sh: need '$tool' installed first (then re-run)" >&2
    exit 1
  fi
done
if ! command -v sha256sum >/dev/null 2>&1 && ! command -v shasum >/dev/null 2>&1; then
  echo "install.sh: need 'sha256sum' or 'shasum' installed first (then re-run)" >&2
  exit 1
fi

REPO="${REPO:-calionauta/gogogo}"
BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"
VERSION="${GOGOGO_VERSION:-latest}"

OS="$(uname -s)"
ARCH="$(uname -m)"
case "$OS" in
  Linux) OS="linux" ;;
  Darwin) OS="darwin" ;;
  *) echo "install.sh: unsupported OS $OS (linux/darwin only)" >&2; exit 1 ;;
esac
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) echo "install.sh: unsupported arch $ARCH (amd64/arm64 only)" >&2; exit 1 ;;
esac

# Release assets are named gogogo-<Version>-<Os>-<Arch>.tar.gz, e.g.
# gogogo-0.32.0-linux-amd64.tar.gz (GoReleaser strips the leading v,
# Os/Arch are raw GOOS/GOARCH). Checksum file: SHA256SUMS.txt.
if [ "$VERSION" = "latest" ]; then
  TAG="$(curl -sSfL -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" | sed 's/.*\///')"
else
  TAG="$VERSION"
fi
VER="$(printf '%s' "$TAG" | sed 's/^v//')"
BASE="https://github.com/$REPO/releases/download/$TAG"
TARBALL="gogogo-$VER-$OS-$ARCH.tar.gz"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT INT TERM
cd "$TMP"
curl -sSfL -o "$TARBALL" "$BASE/$TARBALL"
curl -sSfL "$BASE/SHA256SUMS.txt" | grep " $TARBALL\$" > SHA256SUMS.want

if command -v sha256sum >/dev/null 2>&1; then
  sha256sum -c SHA256SUMS.want
else
  shasum -a 256 -c SHA256SUMS.want
fi

mkdir -p "$BIN_DIR"
tar -xzf "$TARBALL" -C "$BIN_DIR" gogogo gogogo-mcp
chmod +x "$BIN_DIR/gogogo" "$BIN_DIR/gogogo-mcp"

echo "installed: $BIN_DIR/gogogo $BIN_DIR/gogogo-mcp ($TAG)"
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) echo "note: $BIN_DIR is not on PATH — add: export PATH=\"\$BIN_DIR:\$PATH\"" ;;
esac

# Go bootstrap: the scaffold proof (tidy+build) and `make dev` need a
# toolchain, so a Go-less machine would stall right after install.
# Present installs are never touched; only a missing `go` triggers a
# user-space toolchain (rustup-style, ~/.local/go + a $BIN_DIR symlink,
# so the same PATH export covers both).
if [ "${SKIP_GO_BOOTSTRAP:-0}" != "1" ] && ! command -v go >/dev/null 2>&1; then
  GO_DIR="${GO_DIR:-$HOME/.local/go}"
  if [ -x "$GO_DIR/bin/go" ]; then
    echo "go: using existing user-space toolchain at $GO_DIR"
  else
    GO_VERSION="$(curl -sSfL 'https://go.dev/dl/?mode=json' |
      grep -o '"version": *"go[0-9][0-9.]*"' | head -n 1 |
      sed 's/.*go//;s/"//g')"
    if [ -z "$GO_VERSION" ]; then
      echo "install.sh: could not resolve a Go version (offline?) — install Go from https://go.dev/dl/, then run: gogogo --run" >&2
      exit 1
    fi
    echo "go: no toolchain found — bootstrapping go$GO_VERSION into $GO_DIR …"
    mkdir -p "$GO_DIR"
    curl -sSfL -o "$TMP/go.tgz" "https://go.dev/dl/go$GO_VERSION.$OS-$ARCH.tar.gz"
    tar -xzf "$TMP/go.tgz" -C "$TMP"
    rm -rf "$GO_DIR"
    mv "$TMP/go" "$GO_DIR"
    ln -sf "$GO_DIR/bin/go" "$BIN_DIR/go"
    echo "go: bootstrapped $($BIN_DIR/go version) (symlinked at $BIN_DIR/go)"
  fi
fi

"$BIN_DIR/gogogo" --version
