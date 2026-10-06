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
# PatchElements selector, or an UNKNOWN_ATTR_TYPO). Warnings do not block, and
# this repo has ~100 of them from attributes that are intentional by design
# (cuelume `data-cuelume-*`, `data-variant`, `data-on:*`) — so
# `-only-errors` keeps every blocking check while staying quiet. Adding those to
# .datastar-lint.yaml instead would silence a real typo of the same shape.
set -uo pipefail

if [ ! -x ./bin/datastar-lint ]; then
  echo "⚠  datastar-lint not found; skipping"
  exit 0
fi

./bin/datastar-lint -only-errors -r --analyzers html,go ./features/ ./web/ ./internal/
