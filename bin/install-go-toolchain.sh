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
# Usage (CI): bash bin/install-go-toolchain.sh   # needs sudo for /usr/local
#   GO_VERSION=1.27.2 is the default; GITHUB_PATH is appended when present.
set -uo pipefail

VERSION="${GO_VERSION:-1.27.2}"
URL="https://go.dev/dl/go${VERSION}.linux-amd64.tar.gz"

for i in 1 2 3; do
	if curl -fsSL "$URL" -o /tmp/go.tgz; then
		break
	fi
	echo "⚠️ toolchain download failed (attempt $i/3)" >&2
	if [ "$i" -lt 3 ]; then
		sleep 15
	else
		echo "❌ toolchain download failed after 3 attempts" >&2
		exit 1
	fi
done
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf /tmp/go.tgz
if [ -n "${GITHUB_PATH:-}" ]; then
	echo "/usr/local/go/bin" >> "$GITHUB_PATH"
fi
/usr/local/go/bin/go version
