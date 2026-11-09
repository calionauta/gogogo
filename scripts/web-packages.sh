#!/usr/bin/env bash
# Lists the package directories that comprise the deployable web application
# and its tests. Directories work with Go tools and golangci-lint alike.
# cmd/desktop is a separate Wails/CGO target with OS-native WebView headers.
# cmd/gui is a separate gogpu/ui native target (pure Go, zero CGO, but
# heavy GPU deps — wgpu/naga — that would slow web CI).
# cmd/mobile is an android-only target (build-tagged, no host package).
# cmd/mygo is a separate MyGo target (pure-Go shell over the same flat
# stack; tursogo link needs CGO on macOS/Linux, like desktop).
# None is compiled by any CI job: they are local targets (`make desktop`,
# `make gui`, `make mobile`, `make mygo`), and docs/desktop-mobile.md plus
# the skill references carry the recipe. They stay out of this list so web
# CI keeps a single, fast package set.

set -euo pipefail

project_dir=$(pwd)
go list -f '{{.Dir}}' ./... | grep -vE "/cmd/(desktop|gui|mobile|mygo)$|^${project_dir}$"
