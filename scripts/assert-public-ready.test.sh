#!/usr/bin/env bash
# assert-public-ready.test.sh — the contract of the public readiness gate.
#
# The gate exists because the origin and the public path are different claims:
# a container can be healthy on 127.0.0.1 while the tunnel (or the vhost, or
# a container that has not finished binding) still fails the user. A deploy
# that only proves the origin reports green while the site is down.
#
# These cases pin the three properties that make the gate usable:
#   1. it passes when the public path serves the expected build;
#   2. it FAILS when the public path serves a different build (the old
#      container is still answering through the tunnel);
#   3. it FAILS when the public path is broken (502 / connection refused),
#      and it does not hang — a half-open tunnel must not stall the deploy.
#
# The server is a local stub: the gate takes a base URL, so the behaviour can
# be tested without a tunnel, a registry, or a deploy.
set -euo pipefail

SCRIPT="$(cd "$(dirname "$0")" && pwd)/assert-public-ready.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

fail() {
    echo "✗ $1" >&2
    exit 1
}

# ── A stub origin that answers /health with a chosen build stamp ──
start_stub() {
    local port="$1" build="$2" mode="$3"
    cat > "${TMP}/stub-${port}.py" <<PY
import http.server, sys

BUILD = "${build}"
MODE = "${mode}"

class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if MODE == "broken":
            self.send_response(502)
            self.end_headers()
            self.wfile.write(b"bad gateway")
            return
        if MODE == "refuse":
            self.close_connection = True
            return
        self.send_response(200)
        self.send_header("Content-Type", "text/plain")
        self.send_header("X-Gogogo-Build", BUILD)
        self.end_headers()
        self.wfile.write(b"ok")

    def log_message(self, *a):
        pass

http.server.HTTPServer(("127.0.0.1", ${port}), H).serve_forever()
PY
    # The stub's stdout must NOT be the command-substitution pipe: a
    # background process holding the pipe open makes $(...) wait for EOF, and
    # the script would hang before the first assertion. Redirect to the log.
    python3 "${TMP}/stub-${port}.py" >"${TMP}/stub-${port}.log" 2>&1 &
    echo $!
}

wait_for_port() {
    local port="$1"
    for _ in $(seq 1 50); do
        if curl -fsS -o /dev/null --max-time 1 "http://127.0.0.1:${port}/health" 2>/dev/null; then
            return 0
        fi
        sleep 0.1
    done
    return 1
}

# ── 1. the happy path ──
PORT_OK=18901
PID_OK="$(start_stub "${PORT_OK}" "1.2.3-abc1234" ok)"
wait_for_port "${PORT_OK}" || fail "stub did not come up"

if ! bash "${SCRIPT}" "abc1234" "http://127.0.0.1:${PORT_OK}" 3 >/dev/null 2>&1; then
    kill "${PID_OK}" 2>/dev/null || true
    fail "expected success when the public path serves the expected build"
fi
echo "  ✓ passes when the public path serves the expected build"
kill "${PID_OK}" 2>/dev/null || true

# ── 2. the wrong build is a FAILURE, not a warning ──
PORT_OLD=18902
PID_OLD="$(start_stub "${PORT_OLD}" "1.2.3-oldbuild" ok)"
wait_for_port "${PORT_OLD}" || fail "stub did not come up"

if bash "${SCRIPT}" "newbuild" "http://127.0.0.1:${PORT_OLD}" 2 >/dev/null 2>&1; then
    kill "${PID_OLD}" 2>/dev/null || true
    fail "expected failure when the public path serves a different build"
fi
echo "  ✓ fails when the public path serves a different build"
kill "${PID_OLD}" 2>/dev/null || true

# ── 3. a broken public path is a FAILURE and does not hang ──
PORT_502=18903
PID_502="$(start_stub "${PORT_502}" "1.2.3-abc1234" broken)"
sleep 1

START="$(date +%s)"
if bash "${SCRIPT}" "abc1234" "http://127.0.0.1:${PORT_502}" 2 >/dev/null 2>&1; then
    kill "${PID_502}" 2>/dev/null || true
    fail "expected failure when the public path answers 502"
fi
ELAPSED=$(( $(date +%s) - START ))
[ "${ELAPSED}" -lt 30 ] || { kill "${PID_502}" 2>/dev/null || true; fail "the gate hung for ${ELAPSED}s on a broken path"; }
echo "  ✓ fails (without hanging) when the public path answers 502"
kill "${PID_502}" 2>/dev/null || true

# ── 4. a refused connection is a FAILURE, not a crash ──
PORT_DOWN=18904
if bash "${SCRIPT}" "abc1234" "http://127.0.0.1:${PORT_DOWN}" 2 >/dev/null 2>&1; then
    fail "expected failure when the public path refuses connections"
fi
echo "  ✓ fails when the public path refuses connections"

# ── 5. usage errors are distinct from failures ──
if bash "${SCRIPT}" >/dev/null 2>&1; then
    fail "expected a usage error with no arguments"
fi
echo "  ✓ usage error without arguments"

echo "✅ assert-public-ready contract holds"
