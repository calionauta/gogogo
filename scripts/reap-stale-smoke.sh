#!/usr/bin/env bash
# reap-stale-smoke.sh — delete OUR smoke artifacts older than STALE_AFTER_MIN.
#
# Why: gates leave corpses when they die mid-run (SIGTERM/SIGKILL skips every
# happy-path `rm`: Go TempDirs, SMOKE_BIN, ci-fanout logs). `make smoke-clean`
# removes them, but it is manual and nobody runs it after a kill -9.
#
# Safety contract (so this never disturbs a live run or forces a rebuild):
#   * age-gated: only paths with mtime older than SMOKE_STALE_MIN (default
#     360 — a gate takes minutes, so a 6h margin never touches a live run,
#     even a second agent's concurrent gate on this shared host).
#   * allow-listed names only (our tmp patterns + the fixed SMOKE_BIN name).
#   * caches are NEVER listed: go build, golangci-lint, npm, module cache,
#     Playwright browsers. Deleting those would force full rebuilds.
#
# Usage: bash scripts/reap-stale-smoke.sh   (called at the start of
#   `make ci-local` / `make ci-local-fast`; safe to run by hand any time)
set -euo pipefail

STALE_AFTER_MIN=${SMOKE_STALE_MIN:-360}
tmpbase="${OS_TMPDIR:-${TMPDIR:-/tmp}}"

# Fixed SMOKE_BIN default from the Makefile (overridable per-invocation, but
# the default is the path concurrent local runs share — and therefore the
# one whose corpses matter). Only reaped when stale (see contract above).
reaped=0
for pattern in 'gogogo-smoke-*' 'gogogo-prod-smoke-*' 'wb-client-test-*' 'notes-caret-test-*' 'gogogo-docwatch-*' 'gogogo-ci-logs' 'gogogo-ci-local-web'; do
  while IFS= read -r stale; do
    [ -z "$stale" ] && continue
    rm -rf "$stale"
    echo "  reaped stale $stale"
    reaped=1
  done < <(find "$tmpbase" -maxdepth 1 -name "$pattern" -mmin +"$STALE_AFTER_MIN" 2>/dev/null || true)
done
[ "$reaped" -eq 0 ] && echo "  (no stale smoke artifacts)"
