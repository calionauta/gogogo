#!/usr/bin/env bash
# Runs the web test suite with the right amount of parallelism.
#
# Why not a single `-p 1`: the DagNats embedded engine (NATS + durable
# workflow) starves under parallel packages under -race, so THOSE packages
# must serialize. Everything else has no such constraint, and serializing
# them wastes minutes (features/todo alone is ~100s of the run).
#
# Strategy: split into
#   - ENGINE packages  -> `-p 1`, run in the foreground
#   - the rest         -> default parallelism (-p = NumCPU), run concurrently
# Both run at the same time, so the parallel group hides under the engine's
# wall-clock. Measured ~2m09 vs ~4m15 for the naive single `-p 1` sweep, and
# stable across repeated runs.
#
# Usage: bash scripts/test-web.sh [extra go test flags]
#   bash scripts/test-web.sh -race -count=1            # the gate's mode
#   bash scripts/test-web.sh -count=1                  # test-fast (no race)
set -uo pipefail

extra=("$@")
all=$(bash scripts/web-packages.sh)

engine=""
rest=""
for p in $all; do
  # A package that boots the DagNats engine in its tests needs -p 1.
  if grep -rlq 'dagnats\.NewServer\|dagnats\.New\b' "$p"/*_test.go 2>/dev/null; then
    engine="$engine $p"
  else
    rest="$rest $p"
  fi
done

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

rc=0

# Parallel group first (background), engine group in the foreground.
if [ -n "$rest" ]; then
  # shellcheck disable=SC2086
  go test "${extra[@]}" $rest >"$tmpdir/rest.log" 2>&1 &
  rest_pid=$!
fi

if [ -n "$engine" ]; then
  # shellcheck disable=SC2086
  go test -p 1 "${extra[@]}" $engine >"$tmpdir/engine.log" 2>&1
  engine_rc=$?
  [ "$engine_rc" -ne 0 ] && rc=1
fi

if [ -n "$rest" ]; then
  wait "$rest_pid" || rc=1
fi

[ -f "$tmpdir/engine.log" ] && cat "$tmpdir/engine.log"
[ -f "$tmpdir/rest.log" ] && cat "$tmpdir/rest.log"
exit "$rc"
