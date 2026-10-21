#!/usr/bin/env bash
# assert-deploy-build.sh — fail-closed check that production serves the commit
# this deploy intended.
#
# Usage: assert-deploy-build.sh <expected-commit> [health-url]
#   expected-commit  short or full git SHA the artifact was built from
#                    (the deploy passes $GITHUB_SHA; a manual run can pass
#                    `git -C repo rev-parse --short HEAD`)
#   health-url       default http://127.0.0.1:8080/health
#
# Why this exists: compose reports success when the *container* is up, which is
# not the same claim as "the artifact I just deployed is the one serving". A
# failed pull, an expired registry login, or a forgotten digest each leave the
# previous container running and healthy while the deploy looks green. The
# build stamp is the only observable that distinguishes the two, so it is a
# hard requirement, not a warning.
#
# Exit codes: 0 match · 1 mismatch or missing header · 2 usage error.
set -euo pipefail

EXPECTED="$(printf '%s' "${1:-}" | tr '[:upper:]' '[:lower:]')"
URL="${2:-http://127.0.0.1:8080/health}"

if [ -z "${EXPECTED}" ]; then
    echo "usage: $0 <expected-commit> [health-url]" >&2
    exit 2
fi

# -D - dumps headers, -o /dev/null throws the body away. `|| true` keeps a
# connection failure on this path so the diagnosis below is the message the
# operator sees, not a bare curl exit code.
HEADERS="$(curl -fsS -D - -o /dev/null --max-time 5 "${URL}" 2>/dev/null || true)"
TAG="$(printf '%s' "${HEADERS}" | tr -d '\r' | awk -F': *' 'tolower($1) == "x-gogogo-build" { print $2; exit }')"

if [ -z "${TAG}" ]; then
    echo "✗ ${URL} did not answer with an X-Gogogo-Build header — cannot prove which build is serving" >&2
    echo "  (is the container running? does /health still set the header?)" >&2
    exit 1
fi

# The header is "<version>-<short sha>"; the sha is the last dash-separated
# token. Prefix matching both ways covers a full SHA on either side.
ACTUAL="$(printf '%s' "${TAG##*-}" | tr '[:upper:]' '[:lower:]')"
if [[ "${ACTUAL}" == "${EXPECTED}"* || "${EXPECTED}" == "${ACTUAL}"* ]]; then
    echo "✓ production serves ${TAG} (expected ${EXPECTED})"
    exit 0
fi

echo "✗ production reports ${TAG} but this deploy built ${EXPECTED}" >&2
echo "  the new artifact did not take effect — the previous container is still serving." >&2
echo "  check: docker inspect ${PROJECT:-gogogo} --format '{{.Image}}' && docker compose ps" >&2
exit 1
