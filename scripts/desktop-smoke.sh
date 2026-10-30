#!/usr/bin/env bash
# desktop-smoke.sh — prove the Wails shell actually opens and loads the app.
#
# `make desktop` only compiles. No CI job compiles this target either, and the
# shell can run "healthy" with no window at all (app.Run() creates none), so the
# gate has to observe the WINDOW: the webview's own requests show up in the flat
# stack's access log. That is what this script asserts, with no screen
# recording, no AppleScript and no display automation:
#
#   1. the backend boots on an ephemeral port (a fixed :8080 is often taken)
#   2. the window is created and the webview fetches / and /static/app.min.css
#   3. first-run signup opens the door when the database has no account
#   4. a second launch exits instead of opening a second backend
#
# Usage:
#   ./scripts/desktop-smoke.sh              # build, then smoke
#   SKIP_BUILD=1 ./scripts/desktop-smoke.sh # reuse build/desktop/gogogo-desktop
#   KEEP=1 ./scripts/desktop-smoke.sh       # keep the temp dir for inspection
#
# Requires a GUI session (the app opens a window): macOS or Linux with a
# display. Not wired into CI for the same reason nothing else here is.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="${SMOKE_BIN:-$ROOT/build/desktop/gogogo-desktop}"
TIMEOUT_S="${TIMEOUT_S:-30}"

info()  { printf '  → %s\n' "$*"; }
fail()  { printf '✗ %s\n' "$*" >&2; exit 1; }

[ "$(uname -s)" = "Darwin" ] || [ -n "${DISPLAY:-}${WAYLAND_DISPLAY:-}" ] \
  || fail "no GUI session: the shell opens a real window (run it on a desktop host)"

if [ "${SKIP_BUILD:-0}" != "1" ]; then
  make -C "$ROOT" desktop
fi
[ -x "$BIN" ] || fail "missing $BIN (run 'make desktop' first)"

TMP="$(mktemp -d "${TMPDIR:-/tmp}/gogogo-desktop-smoke.XXXXXX")"
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

# One profile per run: the temp database also scopes the single-instance lock,
# so this never collides with a desktop the user already has open.
export HOST=127.0.0.1
# PORT is deliberately NOT set: the shell must bind an ephemeral port on its
# own, even though the default :8080 is usually taken on a dev machine.
export TURSO_DB_PATH="$TMP/app.db"
export DATA_DIR="$TMP/data"
export NATS_STORE_DIR="$TMP/nats"
export DAGNATS_ENABLED=false
export GOAKT_ENABLED=false
export HTTP_ACCESS_LOG=true
# Production-like defaults: signup closed. First-run bootstrap has to open it.
export SIGNUP_ENABLED=false

info "launching $BIN"
"$BIN" >"$TMP/run.log" 2>&1 &
PID=$!

wait_for() { # wait_for <log> <regex> <what>
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

wait_for "$TMP/run.log" "desktop: window on flat stack" "the window was created"
ADDR="$(sed -nE 's/.*"desktop: window on flat stack".*"addr":"([^"]+)".*/\1/p' "$TMP/run.log" | tail -1)"
[ -n "$ADDR" ] || fail "could not read the bound address from the log"

# Ephemeral, not the fixed default: with PORT unset the address must never be
# :8080 (that is the value this check exists to catch), and it must not keep the
# :0 placeholder either.
case "$ADDR" in
  *:0) fail "listener kept the :0 placeholder ($ADDR)" ;;
  *:8080) fail "the shell bound the fixed default :8080 instead of an ephemeral port ($ADDR)" ;;
esac
info "backend bound $ADDR"

wait_for "$TMP/run.log" 'first-run signup enabled' "first-run signup bootstrap"

# The webview, not this script, made these requests: the Go process is the only
# client of its own loopback origin, so a request proves the window rendered.
wait_for "$TMP/run.log" '"method":"GET","path":"/"' "the webview to load /"
wait_for "$TMP/run.log" '"/static/app.min.css"' "the webview to load the CSS bundle"
info "webview loaded the app"

# A second launch must lose the single-instance race BEFORE touching the
# backend: the database file lock would also stop it, but only after a scary
# "File is locked by another process" boot failure. Clean exit 0 is the gate.
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


# --- distribution gate: LICENSE_MODE=license must block until activated -----
info "checking the activation gate"
kill "$PID" 2>/dev/null || true
wait "$PID" 2>/dev/null || true
PID=""

PUB="$(cd "$ROOT" && go run ./cmd/license keygen -out "$TMP/license.key" 2>/dev/null | sed -n 's/^LICENSE_PUBLIC_KEY=//p')"
[ -n "$PUB" ] || fail "cmd/license keygen produced no public key"

LICENSE_MODE=license LICENSE_PUBLIC_KEY="$PUB" "$BIN" >"$TMP/licensed.log" 2>&1 &
PID=$!
wait_for "$TMP/licensed.log" '"path":"/activate"' "the gate to redirect the window to /activate"
ADDR2="$(sed -nE 's/.*"desktop: window on flat stack".*"addr":"([^"]+)".*$/\1/p' "$TMP/licensed.log" | tail -1)"
[ -n "$ADDR2" ] || fail "could not read the licensed run address"

PRE="$(curl -s -o /dev/null -w '%{http_code}' "http://$ADDR2/")"
[ "$PRE" = "303" ] || fail "unactivated app served / without redirecting (status $PRE)"

TOKEN="$(cd "$ROOT" && go run ./cmd/license mint -key "$TMP/license.key" -plan pro 2>/dev/null | tail -1)"
[ -n "$TOKEN" ] || fail "cmd/license mint produced no token"
ACT="$(curl -s -o /dev/null -w '%{http_code}' -X POST --data-urlencode "token=$TOKEN" "http://$ADDR2/activate")"
[ "$ACT" = "303" ] || fail "activation returned $ACT, want 303"
POST="$(curl -s -o /dev/null -w '%{http_code}' "http://$ADDR2/")"
[ "$POST" = "200" ] || fail "activated app still gated (status $POST)"
info "activation gate opens after a valid license"

printf '✓ desktop-smoke: window, webview, first-run signup, single instance, activation gate\n'
