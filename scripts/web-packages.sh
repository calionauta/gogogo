#!/usr/bin/env bash
# Lists the package directories that comprise the deployable web application
# and its tests. Directories work with Go tools and golangci-lint alike.
# cmd/desktop is a separate Wails/CGO target with OS-native WebView headers;
# it is validated by .github/workflows/desktop.yml instead of web CI.
# cmd/gui is a separate gogpu/ui native target (pure Go, zero CGO, but
# heavy GPU deps — wgpu/naga — that would slow web CI); it is validated
# by the gui-poc job in .github/workflows/desktop.yml instead of web CI.

set -euo pipefail

project_dir=$(pwd)
go list -f '{{.Dir}}' ./... | grep -vE "/cmd/(desktop|gui)$|^${project_dir}$"
