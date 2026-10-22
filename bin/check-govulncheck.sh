#!/bin/bash
if [ ! -f go.mod ]; then
  echo "→ not a Go project, skipping govulncheck"
  exit 0
fi
echo "→ govulncheck (${GOVULNCHECK_WHEN:-pre-push})..."
# `go tool govulncheck`, not `go install ...@latest`: the scanner version is
# pinned by the `tool` directive in go.mod, so this gate runs the version the
# repo declares. Two reasons, both learned the hard way:
#
#   1. An unpinned scanner is an unpinned attack surface inside the step that
#      decides whether a push is safe. The AGENTS.md rule ("NO
#      `go install <tool>@latest` for the gates") exists because an unpinned
#      tool can be OLDER than the toolchain and then misread the module
#      graph — deadcode once reported type-check errors as dead code that way.
#   2. `go tool` builds the scanner from the module cache, so there is nothing
#      to install and nothing to keep warm, and the scanner can never disagree
#      with the toolchain about the module (the same reason `deadcode` and
#      `templ` are pinned this way).
PKGS=$(bash scripts/web-packages.sh)
go tool govulncheck $PKGS || { echo "❌ Vulnerability scan failed"; exit 1; }
echo "  ✓"
