#!/usr/bin/env bash
# ci-fanout.sh — race suite + three Playwright smoke suites in parallel.
#
# Parallel order surfaces UI failures in ~1min instead of after the ~2min
# race suite. Each suite gets its own log under CI_LOG_DIR; the binary
# boot smoke stays serial BEFORE this fan-out as the cheap gate (if the
# binary does not boot, fail in seconds).
#
# Load caveat, measured: the browser waits are wall-clock assertions
# (IndexedDB drains, SSE swaps with 10-20s timeouts) and CAN starve while
# the race suite hogs the machine — 2 failures in 4 fan-out runs, 0 in 3
# standalone runs, all at offline-queue waits, all green on retry. So a
# failed browser suite gets exactly one serial retry with the parallel
# load gone: pass-on-retry is labeled a flake, two consecutive failures
# mean the product regressed. The retry run records a Playwright trace
# (SMOKE_TRACE) so the flake keeps its timeline. The race suite never
# retries (a retried race is a hidden data race).
#
# Ports are collision-free by construction: race tests bind ephemeral
# ports, smoke.mjs takes 8099, whiteboard 8131, notes 8132.
#
# Usage: CI_LOG_DIR=<dir> SMOKE_BIN=<binary> bash scripts/ci-fanout.sh
set -uo pipefail
logdir="${CI_LOG_DIR:?CI_LOG_DIR must be set}"
bin="${SMOKE_BIN:?SMOKE_BIN must be set}"
mkdir -p "$logdir"

# Interrupt hygiene: if make is killed (SIGTERM/SIGINT — a wedged box, a
# Ctrl-C), the four suites below would otherwise keep running orphaned,
# holding their ports and burning the CPU the gate just freed. Kill them
# with us. Firing after a normal run is harmless (PIDs already reaped).
pid_race= pid_smoke= pid_wb= pid_notes=
trap 'kill $pid_race $pid_smoke $pid_wb $pid_notes 2>/dev/null || true' INT TERM

bash scripts/test-web.sh -race -count=1 >"$logdir/race.log" 2>&1 &
pid_race=$!
SMOKE_BIN="$bin" node scripts/smoke.mjs >"$logdir/smoke.log" 2>&1 &
pid_smoke=$!
SMOKE_BIN="$bin" node scripts/whiteboard-client.test.mjs >"$logdir/whiteboard.log" 2>&1 &
pid_wb=$!
SMOKE_BIN="$bin" node scripts/notes-caret.test.mjs >"$logdir/notes.log" 2>&1 &
pid_notes=$!

# retry_browser reruns one failed browser suite serially: the parallel
# load is gone by then, so a pass means "starved", not "broken". One
# retry only — a second failure is the verdict, not a third run.
retry_browser() {
  local name="$1" script="$2"
  echo "  ↻ $name failed under load, retrying once serially…"
  if SMOKE_BIN="$bin" SMOKE_TRACE="$logdir/$name.retry.trace.zip" node "scripts/$script" >"$logdir/$name.retry.log" 2>&1; then
    echo "  ⚠️ $name passed on retry (flake under parallel load — see $logdir/$name.retry.log)"
  else
    echo "  ❌ $name FAILED twice (see $logdir/$name.log and $logdir/$name.retry.log)"
    failed_logs="$failed_logs $logdir/$name.retry.log"
    return 1
  fi
}

browser_script() {
  case "$1" in
    smoke) echo "smoke.mjs" ;;
    whiteboard) echo "whiteboard-client.test.mjs" ;;
    notes) echo "notes-caret.test.mjs" ;;
  esac
}

fail=0
failed_logs=""
for spec in "race:$pid_race" "smoke:$pid_smoke" "whiteboard:$pid_wb" "notes:$pid_notes"; do
  name="${spec%%:*}"
  pid="${spec##*:}"
  if wait "$pid"; then
    echo "  ✅ $name passed (log: $logdir/$name.log)"
  elif [ "$name" = "race" ]; then
    echo "  ❌ $name FAILED (see $logdir/$name.log)"
    fail=1
    failed_logs="$failed_logs $logdir/$name.log"
  else
    if ! retry_browser "$name" "$(browser_script "$name")"; then
      fail=1
      failed_logs="$failed_logs $logdir/$name.log"
    fi
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
# Green run: nobody reads these logs afterwards (no caller references
# CI_LOG_DIR again, and CI runs the suites as separate steps, not via this
# script), so remove them — a red run keeps everything above for diagnosis.
# The basename guard keeps a mistyped CI_LOG_DIR from nuking an unrelated
# path; caches (go build, golangci, npm, Playwright browsers) are never
# listed here, so this never forces a rebuild.
case "${logdir##*/}" in
  gogogo-ci-logs) rm -rf "$logdir" ;;
  *) echo "  (keeping logs: unexpected CI_LOG_DIR name $logdir)" ;;
esac
echo "✅ fan-out: all suites green"
