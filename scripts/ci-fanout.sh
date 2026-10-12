#!/usr/bin/env bash
# ci-fanout.sh — race suite + three Playwright smoke suites in parallel.
#
# Browser work and Go work do not compete for the same resource (the
# Playwright suites mostly idle-wait on event probes now), so serial
# order only added wall-clock: a UI failure surfaced after the ~2min
# race suite instead of in ~1min. Each suite gets its own log under
# CI_LOG_DIR; the binary boot smoke stays serial BEFORE this fan-out
# as the cheap gate (if the binary does not boot, fail in seconds).
#
# Ports are collision-free by construction: race tests bind ephemeral
# ports, smoke.mjs takes 8099, whiteboard 8131, notes 8132.
#
# Usage: CI_LOG_DIR=<dir> SMOKE_BIN=<binary> bash scripts/ci-fanout.sh
set -uo pipefail
logdir="${CI_LOG_DIR:?CI_LOG_DIR must be set}"
bin="${SMOKE_BIN:?SMOKE_BIN must be set}"
mkdir -p "$logdir"

bash scripts/test-web.sh -race -count=1 >"$logdir/race.log" 2>&1 &
pid_race=$!
SMOKE_BIN="$bin" node scripts/smoke.mjs >"$logdir/smoke.log" 2>&1 &
pid_smoke=$!
SMOKE_BIN="$bin" node scripts/whiteboard-client.test.mjs >"$logdir/whiteboard.log" 2>&1 &
pid_wb=$!
SMOKE_BIN="$bin" node scripts/notes-caret.test.mjs >"$logdir/notes.log" 2>&1 &
pid_notes=$!

fail=0
failed_logs=""
for spec in "race:$pid_race" "smoke:$pid_smoke" "whiteboard:$pid_wb" "notes:$pid_notes"; do
  name="${spec%%:*}"
  pid="${spec##*:}"
  if wait "$pid"; then
    echo "  ✅ $name passed (log: $logdir/$name.log)"
  else
    echo "  ❌ $name FAILED (see $logdir/$name.log)"
    fail=1
    failed_logs="$failed_logs $logdir/$name.log"
  fi
done
if [ "$fail" -ne 0 ]; then
  echo "--- failing suite tails ---"
  for log in $failed_logs; do
    echo "=== $log ==="
    tail -n 30 "$log"
  done
  exit 1
fi
echo "✅ fan-out: all suites green"
