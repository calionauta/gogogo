#!/usr/bin/env bash
# check-run-browser-suite.test.sh — the contract of the browser-suite wrapper.
#
# WHY THIS EXISTS. scripts/run-browser-suite.sh makes three promises, and until
# this file existed NOTHING checked any of them:
#
#   1. A failure is reported by its MARKER, not by position in the log.
#      The wrapper used `tail -n 25`, which shows the NOISE end. Measured on CI
#      run 43870137994: notes-caret ends with 23 consecutive ✓ lines, so the
#      step announced "FAILED twice" and printed zero failed assertions; 33 of
#      that suite's 81 assertions run before the tail window even starts. A
#      regression in the FIRST assertion of any suite was invisible.
#
#   2. EVERY attempt is traced, the failed one included.
#      The header claimed the retry's trace preserved "the only evidence of what
#      the first run was doing", while the code passed a trace path ONLY to the
#      retry (run_suite "$retry_log" "$trace" vs run_suite "$first_log"). The
#      trace therefore recorded the run that PASSED — the one with nothing in it.
#
#   3. The suites CI runs actually HONOUR SMOKE_TRACE.
#      The wrapper exported it for all five; only smoke.mjs and
#      chat-scroll.test.mjs read it. For the other three the wrapper printed
#      "trace: <path>" and wrote an empty promise. A diagnostic that reports
#      itself as present while collecting nothing is worse than none, because
#      the next reader stops looking — so this is asserted, not assumed.
#
# The failure-path and trace-name behaviour is exercised against a FIXTURE suite
# (a real node script that prints the same markers), so this runs in about a
# second with no browser, no server and no network.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$HERE/.." && pwd)"
WRAPPER="$REPO/scripts/run-browser-suite.sh"
TMP="$(mktemp -d)"
# The wrapper resolves the suite as "scripts/$name", so the fixture suites have
# to live in scripts/. They are uniquely named and removed on exit.
FIXTURE="$REPO/scripts/.contract-fixture-$$.test.mjs"
SILENT="$REPO/scripts/.contract-silent-$$.test.mjs"
trap 'rm -rf "$TMP" "$FIXTURE" "$SILENT"' EXIT

fail() {
    echo "✗ $1" >&2
    exit 1
}

pass() {
    echo "  ✓ $1"
}

# --- 1. every suite CI runs honours SMOKE_TRACE -------------------------------
#
# The list is DERIVED from the workflow, not hardcoded: a suite added to CI
# without tracing support fails here instead of being discovered mid-flake with
# no timeline. Both the import of the shared tracer and a direct
# process.env.SMOKE_TRACE read count as honouring it.
# `mapfile`/`readarray` are bash 4+; this file must run under the macOS default
# (bash 3.2) as well as CI, so the list is read with a plain while loop.
SUITES=()
while IFS= read -r suite; do
    [ -n "$suite" ] && SUITES+=("$suite")
done < <(grep -oE 'run-browser-suite\.sh [a-z-]+\.(test\.)?mjs' "$REPO/.github/workflows/ci.yml" \
    | awk '{print $2}' | sort -u)

[ "${#SUITES[@]}" -gt 0 ] || fail "no suites found in .github/workflows/ci.yml — the extraction broke"

untraced=()
for suite in "${SUITES[@]}"; do
    file="$REPO/scripts/$suite"
    [ -f "$file" ] || fail "CI runs scripts/$suite but the file does not exist"
    # A comment that merely MENTIONS SMOKE_TRACE must not satisfy this — that is
    # how the check would pass while the suite traces nothing (proven: deleting
    # the tracer import from notes-caret left the string in five comments and an
    # earlier draft of this check went green). So the assertion is on real USE:
    # either the shared tracer is imported AND called, or the suite reads
    # process.env.SMOKE_TRACE itself.
    uses_tracer=0
    imports_tracer=0
    grep -qE 'from\s+"\./lib/trace\.mjs"' "$file" && imports_tracer=1
    grep -qE '^\s*(await\s+)?(tracer\.(start|stopAll)|traces\(\))' "$file" && uses_tracer=1
    reads_env=0
    grep -qE 'process\.env\.SMOKE_TRACE' "$file" && reads_env=1
    if ! { [ "$imports_tracer" -eq 1 ] && [ "$uses_tracer" -eq 1 ]; } && [ "$reads_env" -ne 1 ]; then
        untraced+=("$suite")
    fi
done

if [ "${#untraced[@]}" -gt 0 ]; then
    fail "these suites are run by CI but never USE the trace (a comment mentioning SMOKE_TRACE does not count), so the path the wrapper advertises collects nothing: ${untraced[*]}"
fi
pass "all ${#SUITES[@]} CI suites actually trace (shared tracer called, or SMOKE_TRACE read) (${SUITES[*]})"

# --- 2. the shared tracer writes one file per label --------------------------
#
# Exercised for real: two fake contexts, two stops, two files. This is the
# property that makes multi-context suites (two users, per-case contexts) keep
# every context's evidence instead of the last stop overwriting the rest.
node --input-type=module -e '
import { traces } from "'"$REPO"'/scripts/lib/trace.mjs";
const written = [];
const fakeCtx = (name) => ({
  tracing: {
    start: async () => {},                                  // must exist: the tracer calls it
    stop: async ({ path }) => written.push(path),
  },
});
const t = traces();
if (t.enabled) { console.error("tracer enabled without SMOKE_TRACE"); process.exit(1); }
await t.start(fakeCtx("a"), "a");   // inert: no SMOKE_TRACE
await t.stopAll();
if (written.length !== 0) { console.error("inert tracer wrote " + written.length + " trace(s)"); process.exit(1); }
' || fail "the tracer is not inert without SMOKE_TRACE (it must not write in a bare run)"
pass "the tracer is inert when SMOKE_TRACE is unset"

SMOKE_TRACE="$TMP/t.trace.zip" node --input-type=module -e '
import { traces } from "'"$REPO"'/scripts/lib/trace.mjs";
const written = [];
const fakeCtx = () => ({
  tracing: {
    start: async () => {},
    stop: async ({ path }) => written.push(path),
  },
});
const t = traces();
if (!t.enabled) { console.error("tracer disabled WITH SMOKE_TRACE"); process.exit(1); }
await t.start(fakeCtx(), "a");
await t.stopAll();
await t.start(fakeCtx(), "b");
await t.stopAll();
if (written.length !== 2) { console.error("expected 2 traces, got " + written.length); process.exit(1); }
if (written[0] === written[1]) { console.error("two contexts shared one path: " + written[0]); process.exit(1); }
' || fail "the tracer does not write one distinct file per labelled context"
pass "the tracer writes a distinct file per context (no stop overwrites another)"

# A context that cannot be traced must SAY SO. The three suites this module was
# added for each silently ignored SMOKE_TRACE for months; a tracer whose
# start() fails quietly rebuilds that trap one layer down.
SMOKE_TRACE="$TMP/y.trace.zip" node --input-type=module -e '
import { traces } from "'"$REPO"'/scripts/lib/trace.mjs";
const t = traces();
await t.start({ tracing: { start: async () => { throw new Error("context already closed"); } } }, "broken");
' 2>"$TMP/warn.txt" || true
grep -q 'trace NOT started' "$TMP/warn.txt" \
    || fail "a trace that failed to start was swallowed silently (the exact trap this module removes)"
pass "a trace that fails to start is reported, not swallowed"

# --- 3. the failure report shows the MARKER, from anywhere in the log --------
#
# The fixture reproduces the exact shape that hid CI run 43870137994's failure:
# the failing assertion comes FIRST, followed by 23 green lines. Any positional
# tail shows only green; only marker extraction shows the failure.
{
    echo 'console.error("  ✗ the FIRST assertion failed: caret jumped a line");'
    for i in $(seq 1 23); do echo "console.log(\"  ✓ green assertion number $i\");"; done
    echo 'console.error("");'
    echo 'console.error("❌ notes client tests failed (1)");'
    echo 'process.exitCode = 1;'
} > "$FIXTURE"

mkdir -p "$TMP/logs"
out="$(
    cd "$REPO" &&
    CI_LOG_DIR="$TMP/logs" SUITE_NAME=fake \
    bash "$WRAPPER" --attempts=1 "$(basename "$FIXTURE")" 2>&1 || true
)"

case "$out" in
    *"the FIRST assertion failed"*) ;;
    *) fail "the failure report dropped the failed assertion (this is CI run 43870137994 all over again):
$out" ;;
esac
pass "the failure report names the failed assertion even when 23 green lines follow it"

# A log with NO marker is a suite that died before reporting (a throw, a kill),
# and must be called out rather than silently shown as a green tail.
echo 'console.log("  ✓ looks fine but died"); process.exit(1);' > "$SILENT"
out="$(
    cd "$REPO" &&
    CI_LOG_DIR="$TMP/logs" SUITE_NAME=silent \
    bash "$WRAPPER" --attempts=1 "$(basename "$SILENT")" 2>&1 || true
)"
case "$out" in
    *"no ✗/❌ marker"*) ;;
    *) fail "a log with no failure marker was not flagged as 'died before reporting':
$out" ;;
esac
pass "a log with no marker is reported as a suite that died before reporting"

# --- 4. the FIRST attempt is the traced one ----------------------------------
#
# The whole point: a retry that passes has nothing to show. Extracting the
# run_suite invocations is the direct assertion — the wrapper must hand a trace
# path to the first attempt, and a DIFFERENT one to the retry.
first_trace="$(grep -nE '^\s*if run_suite "\$first_log"' "$WRAPPER" | grep -c '"\$trace_first"' || true)"
if [ "$first_trace" -ne 1 ]; then
    fail "the FIRST attempt is not traced — the retry's trace records the run that passed:
$(grep -nE 'run_suite "' "$WRAPPER")"
fi
pass "the first attempt is traced, not only the retry"

grep -q 'trace_first=' "$WRAPPER" || fail "no trace_first path is defined"
grep -q 'trace_retry=' "$WRAPPER" || fail "no trace_retry path is defined"
[ "$(grep -c 'trace_first="\$logdir/\$name.trace.zip"' "$WRAPPER")" -eq 1 ] \
    || fail "trace_first is not \${name}.trace.zip (the base path the tracer suffixes)"
[ "$(grep -c 'trace_retry="\$logdir/\$name.retry.trace.zip"' "$WRAPPER")" -eq 1 ] \
    || fail "trace_retry is not \${name}.retry.trace.zip"
# The tracer writes ONE FILE PER CONTEXT (\${base}-<label>.zip), so the wrapper
# must not advertise the bare base path as if a single file lands there.
[ "$(grep -cF '${trace_first%.zip}*.zip' "$WRAPPER")" -ge 1 ] \
    || fail "the flake message announces a bare trace path, but the tracer suffixes one file per context"
pass "the two attempts write distinct trace paths, announced as the per-context glob the tracer writes"

# --- 5. the marker characters are LITERAL, not printf \u escapes -------------
#
# bash 3.2 (macOS default) does NOT expand \u in printf: `printf '\u2717'` emits
# the six characters \u2717 and matches nothing. A wrapper that only works under
# CI's bash 5 is the defect this pins.
if grep -qE "printf '[^']*\\\\u[0-9a-fA-F]{4}" "$WRAPPER"; then
    fail "the wrapper builds a failure marker with printf \\u escapes, which bash 3.2 (macOS) does not expand"
fi
grep -q '✗|❌' "$WRAPPER" || fail "the wrapper's marker regex is not the literal ✗|❌ pair"
pass "failure markers are literal (bash 3.2 compatible)"

echo "✅ run-browser-suite contract holds"
