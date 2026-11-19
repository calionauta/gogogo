#!/bin/bash
# install-go-toolchain.sh — install the pinned Go toolchain, with retries.
#
# Why not actions/setup-go: it extracts the toolchain via tar and has
# intermittently corrupted it on this runner image. Why retries here: even
# the official go.dev tarball dies mid-download with HTTP/2 INTERNAL_ERROR
# (observed: 4 minutes into go1.27.2, curl exit 92), and nothing retries
# curl — a single drop failed the whole job. Version must match the `go`
# directive in go.mod (bumped for GO-2026 stdlib CVEs).
#
# GO_TARBALL_CACHE (optional): a directory the tarball is kept in, so CI can
# restore it with actions/cache instead of paying for it in every job. A hit
# skips the download — which is the point, since the download is both the slow
# part (~5-8s per job, four jobs) and the only part that flakes. A cached file
# that fails `gzip -t` is discarded and re-fetched, so a truncated cache entry
# costs a download instead of a broken toolchain: the tarball is trusted only
# after an integrity check, never on the strength of its name.
#
# Usage (CI): bash bin/install-go-toolchain.sh   # needs sudo for the prefix
#   GO_VERSION=1.27.4 is the default; GITHUB_PATH is appended when present.
#   GO_INSTALL_PREFIX=/usr/local is the install root, overridable so the
#   contract test can install into a temp dir instead of the real /usr/local.
set -uo pipefail

VERSION="${GO_VERSION:-1.27.4}"
NAME="go${VERSION}.linux-amd64.tar.gz"
URL="https://go.dev/dl/${NAME}"
CACHE="${GO_TARBALL_CACHE:-}"
PREFIX="${GO_INSTALL_PREFIX:-/usr/local}"
TGZ="${TMPDIR:-/tmp}/go.tgz"

fetch() {
	for i in 1 2 3; do
		if curl -fsSL "$URL" -o "$TGZ"; then
			return 0
		fi
		echo "⚠️ toolchain download failed (attempt $i/3)" >&2
		if [ "$i" -lt 3 ]; then
			sleep 15
		fi
	done
	echo "❌ toolchain download failed after 3 attempts" >&2
	return 1
}

if [ -n "$CACHE" ]; then
	mkdir -p "$CACHE"
	cached="$CACHE/$NAME"
	if [ -f "$cached" ] && gzip -t "$cached" 2>/dev/null; then
		echo "✓ toolchain tarball from cache: $cached"
		cp "$cached" "$TGZ"
	else
		if [ -f "$cached" ]; then
			echo "⚠️ cached toolchain tarball is corrupt; discarding it" >&2
			rm -f "$cached"
		fi
		fetch || exit 1
		# Only publishers of the cache write to it (see .github/workflows/ci.yml),
		# and a corrupt copy never becomes the cached one.
		if gzip -t "$TGZ" 2>/dev/null; then
			cp "$TGZ" "$cached"
		fi
	fi
else
	fetch || exit 1
fi

sudo rm -rf "$PREFIX/go"
sudo tar -C "$PREFIX" -xzf "$TGZ"
if [ -n "${GITHUB_PATH:-}" ]; then
	echo "$PREFIX/go/bin" >>"$GITHUB_PATH"
fi
"$PREFIX/go/bin/go" version
