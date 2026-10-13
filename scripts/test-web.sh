#!/usr/bin/env bash
# Runs the web test suite with -race, in parallel across packages.
#
# History: this used to split the suite into "engine packages" (forced to
# `-p 1`) and everything else. That split existed because the DagNats tests
# bound FIXED ports (18091/18097/18098/18099), and two packages used the same
# one — internal/nats and features/todo/handlers both grabbed 18099, so under
# `-p N` one lost the bind with "address already in use". It looked like the
# engine needed serialization; it was a port collision.
#
# Now every DagNats test binds an EPHEMERAL HTTP port (`127.0.0.1:0`) and reads
# the real address back from srv.HTTPAddr(). Nothing needs serializing, so this
# is a plain parallel run — measured ~110s vs ~255s for the old blanket `-p 1`
# and ~130s for the split. Verified stable across repeated runs and at
# GOMAXPROCS=2 and =4.
#
# Usage: bash scripts/test-web.sh [extra go test flags]
#   bash scripts/test-web.sh -race -count=1     # the gate's mode
#   bash scripts/test-web.sh -count=1           # test-fast (no race)
set -euo pipefail

# Full-suite guard: this script runs EVERY web package (the T4-equivalent
# workload). On a small shared host that wedges the box — and co-tenant
# agents share these cores — so refuse first, exactly like `make ci-local`.
# Skipped where it cannot bite: GitHub CI runners (CI=true) carry the load
# by design, and ALLOW_HEAVY=1 is the explicit override (same convention
# as CI_LOCAL_FORCE=1 one level up).
if [ "${CI:-}" != "true" ] && [ "${ALLOW_HEAVY:-0}" != "1" ]; then
	bash scripts/check-host-capacity.sh
fi

pkgs=$(bash scripts/web-packages.sh)
# shellcheck disable=SC2086
exec go test "$@" $pkgs
