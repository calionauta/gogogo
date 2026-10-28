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
# Go-test orphans (second loop below) get a LONGER margin (24h): `t.TempDir`
# self-cleans on pass, so survivors are aborts — but `Test*` is a generic
# Go prefix any project on this shared box may use, and a killed gate can
# leave a big tree (installer tests scaffold whole repo copies). 24h means
# no live run can own them (the longest suite is minutes), while a
# concurrent agent's yesterday run is still untouched today.
#
# Usage: bash scripts/reap-stale-smoke.sh   (called at the start of
#   `make ci-local` / `make ci-local-fast`; safe to run by hand any time)
set -euo pipefail

STALE_AFTER_MIN=${SMOKE_STALE_MIN:-360}
TEST_STALE_MIN=${TEST_STALE_MIN:-1440}
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
# Go-test orphans: t.TempDir self-cleans on pass; survivors are aborted
# runs. Longer margin (see contract above) — never a live run.
for pattern in 'Test*' 'go-build*'; do
  while IFS= read -r stale; do
    [ -z "$stale" ] && continue
    rm -rf "$stale"
    echo "  reaped stale $stale"
    reaped=1
  done < <(find "$tmpbase" -maxdepth 1 -name "$pattern" -mmin +"$TEST_STALE_MIN" 2>/dev/null || true)
done

# Playwright browser orphans (third loop, Linux only, self-contained).
#
# The two loops above reap FILES; a leaked browser is a PROCESS TREE. It is the
# one this host died on: a suite that hangs keeps a Chromium tree alive at
# ~600 MB per renderer, those pages land in swap, and once swap hits zero the
# kernel parks co-tenant daemons in D-state. Age is the signal (a dead suite's
# browser exits by itself — Playwright's --remote-debugging-pipe closes — so a
# hang, not a crash, is what survives).
#
# Deliberately not delegating to the host's richer reaper: this file must stay
# self-contained for any checkout, while ~/bin/reap-playwright-orphans.sh is a
# server-blueprint artifact with orphan detection and a 10m host timer. Same
# problem, two scopes; the host one is authoritative. Threshold here is loose
# (60m) because this runs at the start of an ATTENDED gate where a long
# `make ci-local` is legitimate.
#
# comm is allow-listed to Playwright's binary names, so a desktop Chrome on a
# dev machine is never a candidate. /proc is required for per-process age.
BROWSER_STALE_MIN=${BROWSER_STALE_MIN:-60}
if [ -d /proc ]; then
  browser_reaped=0
  while IFS= read -r pid; do
    [ -z "$pid" ] && continue
    if kill -9 "$pid" 2>/dev/null; then
      echo "  reaped stale browser pid $pid"
      browser_reaped=1
    fi
  done < <(
    ps -eo pid=,etimes=,comm= 2>/dev/null \
      | awk -v max="$((BROWSER_STALE_MIN * 60))" \
          '$3 ~ /^(chrome|chrome-headless|chrome_crashpad)$/ && $2 > max { print $1 }'
  )
  [ "$browser_reaped" -eq 0 ] && echo "  (no stale browser processes)"
else
  echo "  (browser reaping skipped: no /proc on this host)"
fi
