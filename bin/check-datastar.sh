#!/bin/bash
# datastar-lint — pre-commit hook for the Datastar surface.
#
# Both analyzers: `html` covers .templ/.html attributes, `go` covers backend SDK
# calls (`sse.PatchElements` and friends). The Go pass costs ~0.05s here — it is
# not a reason to leave the check out.
#
# Paths are the three trees Tailwind scans (features/, web/, internal/) — the
# same set where rendered UI lives. site/ and docs/ are outside it by design.
#
# Exit code: datastar-lint exits 1 when a finding is an ERROR (e.g. a missing
# PatchElements selector). Warnings are reported but do not block — so only
# ERROR-severity rules act as a gate.
set -uo pipefail

if [ ! -x ./bin/datastar-lint ]; then
  echo "⚠  datastar-lint not found; skipping"
  exit 0
fi

./bin/datastar-lint -r --analyzers html,go ./features/ ./web/ ./internal/
