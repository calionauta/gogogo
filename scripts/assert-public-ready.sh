#!/usr/bin/env bash
# assert-public-ready.sh — fail-closed check that the PUBLIC URL serves the
# commit this deploy intended.
#
# Usage: assert-public-ready.sh <expected-commit> <public-base-url> [attempts]
#   expected-commit  short or full git SHA the artifact was built from
#   public-base-url  the public origin the users reach, e.g.
#                    https://gogogo.calionauta.com
#   attempts         how many times to poll (default 20, ~1s apart)
#
# Why this exists, and why it is SEPARATE from assert-deploy-build.sh:
#
#   assert-deploy-build.sh asks the ORIGIN (127.0.0.1) which build is serving.
#   That is necessary — it is what proves the artifact took effect — but it is
#   not sufficient: the origin can be perfectly healthy while the PUBLIC path
#   is still broken. A tunnel that has not reconnected, a Caddy vhost that has
#   not picked up the new upstream, or a container that only just bound its
#   port all produce a window where the origin answers and the public URL
#   does not. That window is exactly what a user hits: the 502s during a
#   deploy came from the gateway ("read: connection reset by peer") while the
#   app itself was fine, and the image was created 5s before the app finished
#   binding its port.
#
#   So the deploy has to check the path the user actually walks. A deploy that
#   only proves the origin is a deploy that can report green while the site is
#   down.
#
# Exit codes: 0 ready · 1 not ready / wrong build · 2 usage error.
set -euo pipefail

EXPECTED="$(printf '%s' "${1:-}" | tr '[:upper:]' '[:lower:]')"
# Strip only the TRAILING slashes: a naive tr -d '/' also eats the scheme's
# slashes ("http://host" -> "http:host"), which turns every probe into a
# connection error.
BASE="${2:-}"
BASE="${BASE%/}"
while [ "${BASE}" != "${BASE%/}" ]; do BASE="${BASE%/}"; done
ATTEMPTS="${3:-20}"

if [ -z "${EXPECTED}" ] || [ -z "${BASE}" ]; then
    echo "usage: $0 <expected-commit> <public-base-url> [attempts]" >&2
    exit 2
fi

# The health endpoint is the cheapest path that still carries the build stamp,
# and it answers without touching the database.
HEALTH="${BASE}/health"

last_reason="no attempt ran"
for attempt in $(seq 1 "${ATTEMPTS}"); do
    # --max-time bounds the whole attempt: a half-open tunnel can hang a
    # request for minutes, and a deploy must not hang on it.
    HEADERS="$(curl -fsS -D - -o /dev/null --max-time 10 "${HEALTH}" 2>/dev/null || true)"
    TAG="$(printf '%s' "${HEADERS}" | tr -d '\r' | awk -F': *' 'tolower($1) == "x-gogogo-build" { print $2; exit }')"

    if [ -z "${TAG}" ]; then
        last_reason="${HEALTH} did not answer with an X-Gogogo-Build header (gateway error, tunnel down, or the app is not serving yet)"
    else
        ACTUAL="$(printf '%s' "${TAG##*-}" | tr '[:upper:]' '[:lower:]')"
        if [[ "${ACTUAL}" == "${EXPECTED}"* || "${EXPECTED}" == "${ACTUAL}"* ]]; then
            echo "✓ public URL ${BASE} serves ${TAG} (expected ${EXPECTED}) after ${attempt} attempt(s)"
            exit 0
        fi
        last_reason="${BASE} serves ${TAG} but this deploy built ${EXPECTED} — the new artifact did not take effect through the public path"
    fi
    [ "${attempt}" -lt "${ATTEMPTS}" ] && sleep 1
done

echo "✗ public URL is not ready: ${last_reason}" >&2
echo "  the origin can be healthy while the public path is not (tunnel, vhost, or a" >&2
echo "  container that has not finished binding). This is the path users walk." >&2
exit 1
