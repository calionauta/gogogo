#!/bin/sh
# install.sh — installs the gogogo installer + MCP server from the latest
# GitHub release into $BIN_DIR (default ~/.local/bin). Verifies SHA256.
# With no Go toolchain on the machine, bootstraps one into ~/.local/go
# (user-space, no sudo, existing installs never touched).
#
#   curl -sSfL https://raw.githubusercontent.com/calionauta/gogogo/master/install.sh | sh
#
# Env: BIN_DIR (install dir), REPO (owner/repo), GOGOGO_VERSION (pin, e.g. v0.32.0),
#      GO_DIR (toolchain dir, default ~/.local/go), SKIP_GO_BOOTSTRAP=1 (offline/tests),
#      GOGOGO_BIN (override the installed binary, for tests).
#
# One-line install + scaffold + dev (humans only — takes the terminal):
#   curl -sSfL https://raw.githubusercontent.com/calionauta/gogogo/master/install.sh | sh -s -- --run
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
# user-space toolchain (rustup-style, into $GO_DIR).
#
# The toolchain is deliberately NOT symlinked into $BIN_DIR as `go`. $BIN_DIR
# is on PATH by design (the two binaries live there), so a `go` symlink would
# make `command -v go` resolve to the bootstrapped toolchain for the whole
# shell — shadowing a system Go (e.g. /usr/local/go, installed later) and, in
# a directory that may be shared, looking like the CLI itself. The toolchain
# is instead reachable where the ecosystem already looks for it:
#   - $GO_DIR/bin on PATH for an auto-updating shell (the export below and
#     the README's);
#   - $HOME/go/bin, which the toolchain's own default GOBIN/install target
#     puts on PATH in ~/.profile via the `go env -w`-era convention.
# An explicit PATH line beats a shadowing symlink: nothing else changes
# meaning, and a later system Go stays authoritative.
if [ "${SKIP_GO_BOOTSTRAP:-0}" != "1" ] && ! command -v go >/dev/null 2>&1; then
  GO_DIR="${GO_DIR:-$HOME/.local/go}"
  bootstrapped_go=""
  if [ -x "$GO_DIR/bin/go" ]; then
    echo "go: using existing user-space toolchain at $GO_DIR"
    bootstrapped_go="$GO_DIR/bin/go"
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
    bootstrapped_go="$GO_DIR/bin/go"
    echo "go: bootstrapped $("$bootstrapped_go" version)"
  fi

  # Put it on THIS process's PATH, so a non-interactive `gogogo ...` exec
  # (and --run's child, and `make dev`'s) can find it. Check reachability
  # BEFORE prepending, or the check is trivially true.
  already_on_path=0
  case ":$PATH:" in
    *":$GO_DIR/bin:"*) already_on_path=1 ;;
  esac
  PATH="$GO_DIR/bin:$PATH"
  export PATH
  if [ "$already_on_path" = "0" ]; then
    echo "go: for future shells, add the toolchain once:"
    echo "      echo 'export PATH=\"$GO_DIR/bin:\$PATH\"' >> ~/.profile   # or your shell rc"
  fi
fi

"$BIN_DIR/gogogo" --version

# One-line mode: after installing, become the scaffold (`--run` takes the
# terminal via /dev/tty because stdin here is the download pipe, not the
# keyboard). Humans only — agents use the printed path, never this.
if [ "${1:-}" = "--run" ]; then
  shift
  # Probe with an external command: a failed redirect on a special
  # builtin aborts POSIX sh outright instead of tripping `if`.
  if ! tty -s </dev/tty >/dev/null 2>&1; then
    echo "install.sh: --run needs an interactive terminal (no pipe/CI)" >&2
    exit 1
  fi
  GOGO_BIN="${GOGOGO_BIN:-$BIN_DIR/gogogo}"
  exec "$GOGO_BIN" --run "$@" </dev/tty
fi
