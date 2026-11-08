#!/usr/bin/env bash
# mygo-smoke.sh — prove the MyGo shell actually opens and loads the app.
#
# `make mygo` only compiles (vet + build, no window needed). No CI job
# compiles this target either, and the shell can run "healthy" with no
# window at all, so the gate has to observe the WINDOW: the webview's own
# requests show up in the flat stack's access log. That is what this
# script asserts, with no screen recording and no display automation:
#
#   1. the backend boots on an ephemeral port (a fixed :8080 is often taken)
#   2. the window is created and the webview fetches / and /static/app.min.css
#   3. first-run signup opens the door when the database has no account
#   4. a second launch exits cleanly (MyGo single-instance lock, app-wide)
#
# Usage:
#   ./scripts/mygo-smoke.sh              # build, then smoke
#   SKIP_BUILD=1 ./scripts/mygo-smoke.sh # reuse build/mygo/gogogo-mygo
#   KEEP=1 ./scripts/mygo-smoke.sh       # keep the temp dir for inspection
#
# Requires a GUI session (the app opens a window): macOS or Linux with a
# display. Not wired into CI for the same reason nothing else here is.
# Activation-gate coverage stays in desktop-smoke (Wails): MyGo shares
# StartChi, so the gate logic is already proved; this script proves the
# MyGo window + single-instance of this shell.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="${SMOKE_BIN:-$ROOT/build/mygo/gogogo-mygo}"
TIMEOUT_S="${TIMEOUT_S:-30}"

info()  { printf '  → %s\n' "$*"; }
fail()  { printf '✗ %s\n' "$*" >&2; exit 1; }

[ "$(uname -s)" = "Darwin" ] || [ -n "${DISPLAY:-}${WAYLAND_DISPLAY:-}" ] \
  || fail "no GUI session: the shell opens a real window (run it on a desktop host)"

if [ "${SKIP_BUILD:-0}" != "1" ]; then
  make -C "$ROOT" mygo
fi
[ -x "$BIN" ] || fail "missing $BIN (run 'make mygo' first)"

TMP="$(mktemp -d "${TMPDIR:-/tmp}/gogogo-mygo-smoke.XXXXXX")"
PID=""

cleanup() {
  if [ -n "$PID" ] && kill -0 "$PID" 2>/dev/null; then
    kill "$PID" 2>/dev/null || true
    wait "$PID" 2>/dev/null || true
  fi
  if [ "${KEEP:-0}" = "1" ]; then
    info "kept $TMP"
  else
    rm -rf "$TMP"
  fi
}
trap cleanup EXIT

export HOST=127.0.0.1
export TURSO_DB_PATH="$TMP/app.db"
export DATA_DIR="$TMP/data"
export NATS_STORE_DIR="$TMP/nats"
export DAGNATS_ENABLED=false
export GOAKT_ENABLED=false
export HTTP_ACCESS_LOG=true
export SIGNUP_ENABLED=false

info "launching $BIN"
"$BIN" >"$TMP/run.log" 2>&1 &
PID=$!

wait_for() {
  local log="$1" deadline=$((SECONDS + TIMEOUT_S))
  while [ "$SECONDS" -lt "$deadline" ]; do
    if grep -qE "$2" "$log" 2>/dev/null; then return 0; fi
    if ! kill -0 "$PID" 2>/dev/null; then
      sed -n '1,80p' "$log" >&2
      fail "process exited before $3"
    fi
    sleep 0.2
  done
  sed -n '1,80p' "$log" >&2
  fail "timed out waiting for $3"
}

wait_for "$TMP/run.log" "mygo: window on flat stack" "the window was created"
ADDR="$(sed -nE 's/.*"mygo: window on flat stack".*"addr":"([^"]+)".*/\1/p' "$TMP/run.log" | tail -1)"
[ -n "$ADDR" ] || fail "could not read the bound address from the log"

case "$ADDR" in
  *:0) fail "listener kept the :0 placeholder ($ADDR)" ;;
  *:8080) fail "the shell bound the fixed default :8080 instead of an ephemeral port ($ADDR)" ;;
esac
info "backend bound $ADDR"

wait_for "$TMP/run.log" "first-run signup enabled" "first-run signup bootstrap"

wait_for "$TMP/run.log" '"method":"GET","path":"/"' "the webview to load /"
wait_for "$TMP/run.log" '"/static/app.min.css"' "the webview to load the CSS bundle"
info "webview loaded the app"

SECOND_START=$SECONDS
"$BIN" >>"$TMP/second.log" 2>&1 &
SECOND=$!
for _ in $(seq 1 50); do
  kill -0 "$SECOND" 2>/dev/null || break
  sleep 0.2
done
if kill -0 "$SECOND" 2>/dev/null; then
  kill "$SECOND" 2>/dev/null || true
  fail "a second instance kept running (single-instance lock missing)"
fi
SECOND_RC=0
wait "$SECOND" 2>/dev/null || SECOND_RC=$?
if [ "$SECOND_RC" -ne 0 ]; then
  sed -n '1,20p' "$TMP/second.log" >&2
  fail "second instance exited $SECOND_RC: it reached the backend instead of the single-instance gate"
fi
if grep -qE "flat stack listening|boot failed" "$TMP/second.log"; then
  sed -n '1,20p' "$TMP/second.log" >&2
  fail "second instance booted the backend before losing the lock"
fi
info "second instance exited cleanly in $((SECONDS - SECOND_START))s"

printf '✓ mygo-smoke: window, webview, first-run signup, single instance\n'
