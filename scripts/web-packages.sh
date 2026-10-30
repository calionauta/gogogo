#!/usr/bin/env bash
# Lists the package directories that comprise the deployable web application
# and its tests. Directories work with Go tools and golangci-lint alike.
# cmd/desktop is a separate Wails/CGO target with OS-native WebView headers.
# cmd/gui is a separate gogpu/ui native target (pure Go, zero CGO, but
# heavy GPU deps — wgpu/naga — that would slow web CI).
# Neither is compiled by any CI job: they are local targets (`make desktop`,
# `make gui`), and docs/desktop-mobile.md plus
# skills/gogogo-coding-standards/references/desktop-native.md carry the recipe.
# They stay out of this list so web CI keeps a single, fast package set.

set -euo pipefail

project_dir=$(pwd)
go list -f '{{.Dir}}' ./... | grep -vE "/cmd/(desktop|gui)$|^${project_dir}$"
