#!/usr/bin/env bash
# run-browser-suite.sh — run ONE Playwright suite with a retry whose pass is
# reported as a FLAKE, never as green.
#
# WHY THIS EXISTS. The retry-with-labeling logic lived only inside
# ci-fanout.sh (the local parallel runner), while CI runs the suites as bare
# `node scripts/<x>.test.mjs` steps — so a flake that passed on retry locally
# simply failed the build in CI, and a flake that passed on retry anywhere was
# only a `⚠️` line in a log nobody reads. One wrapper, used by both runners,
# means "failed once, passed on retry" has ONE meaning everywhere and it is
# visible.
#
# WHY RETRY AT ALL, and what it is NOT. A retry is diagnostic instrumentation,
# not a fix: it separates "the product is broken" from "the machine starved the
# suite". Measured on this repo: 2 browser failures in 4 parallel fan-out runs
# and 0 in 3 standalone runs, all at offline-queue waits, all green on retry —
# i.e. real competition for the box, not a defect. A FAIL-THEN-PASS is reported
# as a flake on stderr and in the GitHub step summary (when running in Actions),
# so it is recorded rather than laundered. A suite that fails TWICE is a
# verdict, not a third run.
#
# The retry records a Playwright trace (SMOKE_TRACE) so a flake keeps its
# timeline: without it the retry log says "it passed" and throws away the only
# evidence of what the first run was doing.
#
# Usage: scripts/run-browser-suite.sh <script> [KEY=VAL ...]
#        scripts/run-browser-suite.sh --attempts=1 <script> [KEY=VAL ...]
#
#   SMOKE_BIN=<binary> scripts/run-browser-suite.sh smoke.mjs
#   SMOKE_BIN=<binary> scripts/run-browser-suite.sh sse-reopen.test.mjs SSE_PROBE_PORT=8133
#
# --attempts=1 runs the suite ONCE and reports the result, with no retry. That
# mode exists for ci-fanout.sh, which has ALREADY run the suite in its parallel
# stage: calling the retrying form there would add two more attempts on top of
# the one that just failed. The fan-out owns "retry with the parallel load gone";
# this script owns "what a failed attempt means and how it is reported".
#
# Exit: 0 = passed (first try or retry), 1 = failed (twice, or once under
# --attempts=1). A flake exits 0 but is never silent.
set -uo pipefail

attempts=2
if [ "${1:-}" = "--attempts=1" ]; then
  attempts=1
  shift
fi

script="${1:?usage: run-browser-suite.sh [--attempts=1] <script> [KEY=VAL ...]}"
shift || true

# Extra env (SSE_PROBE_PORT and friends) is applied to BOTH attempts, so the
# retry exercises the same configuration as the first run.
extra_env=("$@")

# SUITE_NAME pins the log basename. The wrapper otherwise derives it from the
# script (`whiteboard-client.test.mjs` -> `whiteboard-client`), and ci-fanout.sh
# calls the same suite `whiteboard` in its parallel stage — so without the pin,
# the failure message would point at a retry log that does not exist. Callers
# that name their logs themselves pass this through.
name="${SUITE_NAME:-}"
if [ -z "$name" ]; then
  name="${script%.test.mjs}"
  name="${name%.mjs}"
fi

logdir="${CI_LOG_DIR:-}"
if [ -n "$logdir" ]; then
  mkdir -p "$logdir"
  first_log="$logdir/$name.log"
  retry_log="$logdir/$name.retry.log"
  trace="$logdir/$name.retry.trace.zip"
else
  first_log="$(mktemp -t "suites-${name}.XXXXXX.log")"
  retry_log="$(mktemp -t "suites-${name}-retry.XXXXXX.log")"
  trace="$(mktemp -t "suites-${name}-retry.XXXXXX.trace.zip")"
  echo "  (no CI_LOG_DIR: writing to $first_log and $retry_log)"
fi

run_suite() { # run_suite <logfile> [trace-path]
  local log="$1" trace_path="${2:-}"
  if [ -n "$trace_path" ]; then
    env "${extra_env[@]}" SMOKE_TRACE="$trace_path" node "scripts/$script" >"$log" 2>&1
  else
    env "${extra_env[@]}" node "scripts/$script" >"$log" 2>&1
  fi
}

if run_suite "$first_log"; then
  echo "  ✅ $name passed (log: $first_log)"
  exit 0
fi

if [ "$attempts" -eq 1 ]; then
  echo "  ❌ $name FAILED (log: $first_log)"
  tail -n 25 "$first_log" 2>/dev/null || true
  exit 1
fi

echo "  ↻ $name failed, retrying once (a retry is diagnostic: it separates"
echo "     'the product is broken' from 'the machine starved this run')…"
if run_suite "$retry_log" "$trace"; then
  # FLAKE, not green. Loud on stderr so it survives in any log aggregator, and
  # in the Actions step summary so it is visible without opening a log.
  echo "::warning title=Flaky suite ($name)::$name failed once and passed on retry "\
"under the same configuration. Retry log: $retry_log (trace: $trace). This is a "\
"flake, not a fix — the first failure's evidence is in $first_log."
  echo "  ⚠️ $name FLAKY — passed on retry (first: $first_log / retry: $retry_log)"
  if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
    {
      echo "### ⚠️ Flaky suite: \`$name\`"
      echo
      echo "Failed once, passed on retry with the same configuration. Recorded here"
      echo "so a retried pass is never indistinguishable from a clean pass."
      echo
      echo "- first attempt log: \`$first_log\`"
      echo "- retry log: \`$retry_log\`"
      echo "- retry trace: \`$trace\`"
      echo
      echo "If this suite flakes repeatedly, it is a finding about the suite (a"
      echo "wait that syncs on a proxy rather than the effect) — see"
      echo "\`scripts/run-browser-suite.sh\` for the policy."
    } >>"$GITHUB_STEP_SUMMARY"
  fi
  exit 0
fi

echo "  ❌ $name FAILED twice (first: $first_log, retry: $retry_log)"
echo "--- $name first-attempt tail ---"
tail -n 25 "$first_log" 2>/dev/null || true
echo "--- $name retry tail ---"
tail -n 25 "$retry_log" 2>/dev/null || true
exit 1
