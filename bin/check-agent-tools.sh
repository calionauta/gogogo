#!/bin/bash
# check-agent-tools.sh — the AGENTS.md tooling section, enforced.
# ripwire (orient-first maps), cymbal (interactive navigation),
# xerj (docs/prose search) and sem (entity diffs) must all resolve
# before an agent session does real work: a missing tool silently
# degrades the session to grep-and-read, which is how context burns
# and how fixes regress. Fails with the exact install line per tool.
set -uo pipefail

FAIL=0
need() { # name, install-line
	if command -v "$1" >/dev/null 2>&1; then
		echo "  ✓ $1"
	else
		echo "  ❌ $1 missing — install: $2"
		FAIL=1
	fi
}

echo "→ Checking agent tooling (AGENTS.md: ripwire, cymbal, xerj, sem)..."
need ripwire 'RIPWIRE_REPO=redhat-et/ripwire bash -c "$(curl -fsSL https://raw.githubusercontent.com/redhat-et/ripwire/main/scripts/install.sh)"'
need cymbal 'brew install cymbal (then: cymbal index .)'
need xerj 'curl -fsSL https://xerj.org/get | sh (then: xerj autoindex .)'
need sem 'brew install sem-cli (GNU parallel conflict: put brew/cargo bin first in PATH)'

if [ "$FAIL" -ne 0 ]; then
	echo "  install the missing tool(s), then re-run."
	exit 1
fi
echo "  ✅ agent tooling present"
