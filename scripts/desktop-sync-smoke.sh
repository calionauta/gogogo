#!/usr/bin/env bash
# desktop-sync-smoke.sh — prove a desktop install syncs with a real server.
#
# `desktop-smoke.sh` proves the shell opens and the activation gate works.
# This script proves the OTHER half of the distribution story end to end,
# with two real processes and no mocks:
#
#   1. a server in LICENSE_MODE=account issues a device token for an account
#   2. the desktop activates with that token and writes <DATA_DIR>/sync.json
#   3. a restart applies the saved connection as a NATS leaf node
#   4. a todo created on the desktop appears on the server for the SAME user id
#
# The transport is the product's own CRUD wire (app.crud.todo.>), so a green
# run is the integration proof the docs claim. Temp dirs, ephemeral ports,
# token auth; nothing here points at a public host.
#
# Usage:
#   ./scripts/desktop-sync-smoke.sh              # build, then smoke
#   SKIP_BUILD=1 ./scripts/desktop-sync-smoke.sh # reuse both binaries
#   KEEP=1 ./scripts/desktop-sync-smoke.sh       # keep the temp dir
#
# Requires a GUI session: the desktop opens a real window.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DESKTOP_BIN="${SYNC_DESKTOP_BIN:-$ROOT/build/desktop/gogogo-desktop}"
TIMEOUT_S="${TIMEOUT_S:-40}"
SYNC_TIMEOUT_S="${SYNC_TIMEOUT_S:-45}"

info()  { printf '  → %s\n' "$*"; }
pass()  { printf 'PASS: %s\n' "$*"; }
fail()  { printf '✗ %s\n' "$*" >&2; exit 1; }

[ "$(uname -s)" = "Darwin" ] || [ -n "${DISPLAY:-}${WAYLAND_DISPLAY:-}" ] \
  || fail "no GUI session: the desktop opens a real window"

if [ "${SKIP_BUILD:-0}" != "1" ]; then
  make -C "$ROOT" desktop
fi
[ -x "$DESKTOP_BIN" ] || fail "missing $DESKTOP_BIN (run 'make desktop' first)"

TMP="$(mktemp -d "${TMPDIR:-/tmp}/gogogo-desktop-sync.XXXXXX")"
SERVER_PID=""
DESKTOP_PID=""

cleanup() {
  for pid in "$DESKTOP_PID" "$SERVER_PID"; do
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
    fi
  done
  if [ "${KEEP:-0}" = "1" ]; then
    info "kept $TMP"
  else
    rm -rf "$TMP"
  fi
}
trap cleanup EXIT

# free_port asks the kernel for a port and closes it again. The window between
# close and bind is small and both processes bind immediately.
free_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'
}

HTTP_PORT="$(free_port)"
NATS_PORT="$(free_port)"
LEAF_PORT="$(free_port)"
while [ "$NATS_PORT" = "$HTTP_PORT" ] || [ "$LEAF_PORT" = "$HTTP_PORT" ] || [ "$LEAF_PORT" = "$NATS_PORT" ]; do
  NATS_PORT="$(free_port)"
  LEAF_PORT="$(free_port)"
done
NATS_TOKEN="sync-smoke-$(date +%s)-$RANDOM"

wait_for_log() { # wait_for_log <pid> <log> <regex> <what>
  local pid=$1 log=$2 re=$3 what=$4 deadline=$((SECONDS + TIMEOUT_S))
  while [ "$SECONDS" -lt "$deadline" ]; do
    if grep -qE "$re" "$log" 2>/dev/null; then return 0; fi
    if ! kill -0 "$pid" 2>/dev/null; then
      sed -n '1,120p' "$log" >&2
      fail "process exited before $what"
    fi
    sleep 0.2
  done
  sed -n '1,120p' "$log" >&2
  fail "timed out waiting for $what"
}

# --- 1. one Ed25519 key: the server signs, the desktop verifies ------------
info "generating a license key"
KEYGEN_OUT="$(cd "$ROOT" && go run ./cmd/license keygen -out "$TMP/license.key" 2>/dev/null)"
PUB="$(printf '%s\n' "$KEYGEN_OUT" | sed -n 's/^LICENSE_PUBLIC_KEY=//p')"
[ -n "$PUB" ] || fail "cmd/license keygen produced no public key"
PRIV="$(cat "$TMP/license.key")"
[ -n "$PRIV" ] || fail "cmd/license keygen wrote no private key"

# --- 2. the server: account mode, a token-protected NATS listener ---------
info "building the server"
(cd "$ROOT" && go build -o "$TMP/gogogo-web" ./cmd/web)

mkdir -p "$TMP/server/data" "$TMP/server/nats" "$TMP/desktop/data" "$TMP/desktop/nats"
env \
  ENVIRONMENT=production \
  HOST=127.0.0.1 PORT="$HTTP_PORT" \
  APP_URL="http://127.0.0.1:$HTTP_PORT" \
  TURSO_DB_PATH="$TMP/server/data/app.db" \
  DATA_DIR="$TMP/server/data" \
  NATS_STORE_DIR="$TMP/server/nats" \
  NATS_ENABLED=true \
  NATS_LISTEN="127.0.0.1:$NATS_PORT" \
  NATS_LEAFNODE_LISTEN="127.0.0.1:$LEAF_PORT" \
  NATS_TOKEN="$NATS_TOKEN" \
  DAGNATS_ENABLED=false \
  GOAKT_ENABLED=false \
  RBAC_ENABLED=false \
  SIGNUP_ENABLED=true \
  LICENSE_MODE=account \
  LICENSE_PRIVATE_KEY="$PRIV" \
  CLOUD_SYNC_URL="nats://127.0.0.1:$LEAF_PORT" \
  HTTP_ACCESS_LOG=true \
  "$TMP/gogogo-web" >"$TMP/server.log" 2>&1 &
SERVER_PID=$!

deadline=$((SECONDS + TIMEOUT_S))
until curl -sf "http://127.0.0.1:$HTTP_PORT/health" >/dev/null 2>&1; do
  if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    sed -n '1,120p' "$TMP/server.log" >&2
    fail "server exited before it answered /health"
  fi
  [ "$SECONDS" -lt "$deadline" ] || { sed -n '1,120p' "$TMP/server.log" >&2; fail "server never answered /health"; }
  sleep 0.2
done
pass "server booted on 127.0.0.1:$HTTP_PORT with NATS client :$NATS_PORT and leaf :$LEAF_PORT"

# --- 3. create the account, then mint a device token for it ----------------
SIGNUP_CODE="$(curl -s -o "$TMP/signup.json" -w '%{http_code}' \
  -c "$TMP/server-cookies.txt" \
  -H 'Content-Type: application/json' \
  -d '{"email":"sync@example.com","password":"Correct-horse-99!","name":"Sync"}' \
  "http://127.0.0.1:$HTTP_PORT/api/auth/signup/credential")"
[ "$SIGNUP_CODE" = "200" ] || { cat "$TMP/signup.json" >&2; fail "signup returned $SIGNUP_CODE, want 200"; }
pass "account created (200) and session cookie stored"

TOKEN_PAGE_CODE="$(curl -s -o "$TMP/device-token.html" -w '%{http_code}' \
  -b "$TMP/server-cookies.txt" -X POST "http://127.0.0.1:$HTTP_PORT/account/device-token")"
[ "$TOKEN_PAGE_CODE" = "200" ] || fail "device-token returned $TOKEN_PAGE_CODE, want 200"
TOKEN="$(grep -oE 'gogogo1\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+' "$TMP/device-token.html" | head -1)"
[ -n "$TOKEN" ] || { sed -n '1,80p' "$TMP/device-token.html" >&2; fail "device-token page carried no gogogo1 token"; }
SUB="$(python3 - "$TOKEN" <<'PY'
import base64, json, sys
payload = sys.argv[1].split(".")[1]
payload += "=" * (-len(payload) % 4)
print(json.loads(base64.urlsafe_b64decode(payload))["sub"])
PY
)"
[ -n "$SUB" ] || fail "device token has no sub claim"
pass "device token issued for user $SUB"

# --- 4. activate the desktop; it must save the embedded sync endpoint ------
start_desktop() { # start_desktop <log>
  env \
    HOST=127.0.0.1 \
    TURSO_DB_PATH="$TMP/desktop/data/app.db" \
    DATA_DIR="$TMP/desktop/data" \
    NATS_STORE_DIR="$TMP/desktop/nats" \
    DAGNATS_ENABLED=false \
    GOAKT_ENABLED=false \
    SIGNUP_ENABLED=false \
    HTTP_ACCESS_LOG=true \
    LICENSE_MODE=account \
    LICENSE_PUBLIC_KEY="$PUB" \
    LICENSE_FILE="$TMP/desktop/data/license.token" \
    "$DESKTOP_BIN" >"$1" 2>&1 &
  DESKTOP_PID=$!
}

info "launching the desktop (first run)"
start_desktop "$TMP/desktop-1.log"
wait_for_log "$DESKTOP_PID" "$TMP/desktop-1.log" "desktop: window on flat stack" "the desktop window"
ADDR="$(sed -nE 's/.*"desktop: window on flat stack".*"addr":"([^"]+)".*/\1/p' "$TMP/desktop-1.log" | tail -1)"
[ -n "$ADDR" ] || fail "could not read the desktop's bound address"

ACT_CODE="$(curl -s -o "$TMP/activate.html" -w '%{http_code}' \
  -c "$TMP/desktop-cookies.txt" -X POST --data-urlencode "token=$TOKEN" "http://$ADDR/activate")"
[ "$ACT_CODE" = "303" ] || { sed -n '1,80p' "$TMP/activate.html" >&2; fail "activation returned $ACT_CODE, want 303"; }
[ -f "$TMP/desktop/data/sync.json" ] || fail "activation wrote no <DATA_DIR>/sync.json"
python3 - "$TMP/desktop/data/sync.json" <<'PY' || fail "sync.json is missing url/token"
import json, sys
c = json.load(open(sys.argv[1]))
assert c.get("url", "").startswith("nats://127.0.0.1:"), c
assert c["url"].rsplit(":", 1)[1].isdigit(), c
assert c.get("token"), c
PY
pass "desktop activated (303) and saved sync.json with url + token"

# --- 5. restart: the saved connection must become the leaf node -----------
info "restarting the desktop to apply the saved connection"
kill "$DESKTOP_PID" 2>/dev/null || true
wait "$DESKTOP_PID" 2>/dev/null || true
DESKTOP_PID=""

start_desktop "$TMP/desktop-2.log"
wait_for_log "$DESKTOP_PID" "$TMP/desktop-2.log" "desktop: cloud sync configured" "the saved sync endpoint to be applied"
wait_for_log "$DESKTOP_PID" "$TMP/desktop-2.log" "NATS leaf node started" "the leaf node to start"
ADDR="$(sed -nE 's/.*"desktop: window on flat stack".*"addr":"([^"]+)".*/\1/p' "$TMP/desktop-2.log" | tail -1)"
[ -n "$ADDR" ] || fail "could not read the restarted desktop's bound address"
pass "desktop restart applied sync.json and started the NATS leaf node"

# --- 6. create a todo on the desktop, find it on the server ----------------
CREATE_CODE="$(curl -s -o "$TMP/create.out" -w '%{http_code}' \
  -b "$TMP/desktop-cookies.txt" -X POST \
  --data-urlencode "title=Synced from desktop" "http://$ADDR/api/todos")"
[ "$CREATE_CODE" = "200" ] || { sed -n '1,40p' "$TMP/create.out" >&2; fail "desktop todo create returned $CREATE_CODE, want 200"; }
pass "todo created on the desktop (200)"

SYNC_DEADLINE=$((SECONDS + SYNC_TIMEOUT_S))
SYNCED=0
while [ "$SECONDS" -lt "$SYNC_DEADLINE" ]; do
  if curl -s -b "$TMP/server-cookies.txt" \
      "http://127.0.0.1:$HTTP_PORT/api/todos/fragment?filter=all" \
      | grep -q "Synced from desktop"; then
    SYNCED=1
    break
  fi
  sleep 0.5
done
[ "$SYNCED" = "1" ] || {
  sed -n '1,80p' "$TMP/desktop-2.log" >&2
  sed -n '1,80p' "$TMP/server.log" >&2
  fail "server never listed the todo created on the desktop"
}
pass "server API lists the desktop todo for the signed-in account"

# Stop the server before reading its database: a live WAL is not visible to
# a second SQLite reader on this platform, and a clean shutdown also proves
# the row reached disk instead of only the running process's page cache.
kill "$SERVER_PID" 2>/dev/null || true
wait "$SERVER_PID" 2>/dev/null || true
SERVER_PID=""

# The owner id is the convergence key: prove the row is stored under the
# account's id, not a local shadow identity.
python3 - "$TMP/server/data/app.db" "$SUB" <<'PY' || fail "server row is not owned by $SUB"
import sqlite3, sys
db, want = sys.argv[1], sys.argv[2]
# Read-write on purpose: a read-only connection cannot create the WAL
# index (-shm) while the server holds the database, so it would miss the
# row the API just listed. This still only SELECTs.
con = sqlite3.connect(db, timeout=10)
try:
    rows = con.execute(
        "select owner, title from todos where title = ?", ("Synced from desktop",)
    ).fetchall()
finally:
    con.close()
assert rows, "no Synced from desktop row in the server database"
assert all(owner == want for owner, _ in rows), rows
PY
pass "server row is owned by the same user id ($SUB)"

printf '✓ desktop-sync-smoke: account token, sync.json, leaf restart, same-owner CRUD sync\n'
