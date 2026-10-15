#!/bin/bash
# Post-push verifier: confirms what push-to-master actually shipped.
#
# Deploy fires on push without waiting for CI, so the pre-push gate is only
# half the story — this checks the other half (default-branch CI + live
# /health) with a bounded wait, then exits 0/1. Never blocks like
# `gh run watch` (that pattern got an agent idle-killed before).
#
# Engine is the cali-ops-verify-ci skill script (gh + curl only, single
# JSON line out). This wrapper only pins gogogo's slug + live URL.
# Re-run later when it reports pending (CI outruns the 120s cap).
set -euo pipefail

SKILL_SCRIPT="${HOME}/.agents/skills/cali-ops-verify-ci/references/verify-ci.sh"
if [ ! -x "$SKILL_SCRIPT" ]; then
	echo "verify-ci: skill script not found: $SKILL_SCRIPT" >&2
	echo "verify-ci: the cali-ops-verify-ci skill must be installed (see skills reference)." >&2
	exit 2
fi

exec "$SKILL_SCRIPT" "calionauta/gogogo" "https://gogogo.calionauta.com/health"
