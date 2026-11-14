#!/usr/bin/env bash
# operator-look — record the operator's verdict about the running app.
#
# The agent runs this ONLY after the operator has answered about the build
# `make verify-local` booted. It writes `.gogogo/verify-local.json`, the stamp
# `bin/check-operator-look.sh` reads before a signoff.
#
# Why a separate command: the verdict is a human judgment, and the record has
# to name the exact commit it applies to. Keeping it out of the boot script
# means a look can be recorded for a build someone already clicked, and an edit
# after the look invalidates the stamp instead of silently blessing new code.
#
# Usage:
#   scripts/operator-look.sh pass ["what you saw"]
#   scripts/operator-look.sh fail ["what is wrong"]
set -euo pipefail
cd "$(dirname "$0")/.."

verdict="${1:-}"
note="${2:-}"
case "$verdict" in
pass | fail) ;;
*)
	echo "usage: scripts/operator-look.sh pass|fail [\"note\"]" >&2
	exit 2
	;;
esac

commit=$(git rev-parse HEAD)
# The stamp names the WORKTREE, not just the commit: the operator looked at the
# files as they are now, and an edit after the look must invalidate it.
tree=$(
	{
		git diff HEAD
		git ls-files -o --exclude-standard -z -- . ':(exclude).gogogo' | while IFS= read -r -d "" f; do
			printf '== %s\n' "$f"
			git hash-object --no-filters -- "$f"
		done
	} | git hash-object --stdin
)
mkdir -p .gogogo

# Written by node rather than printf so the note is JSON-escaped: a quote in
# what the operator said must not produce a stamp the reader cannot parse.
node -e '
const fs = require("fs");
const [commit, tree, verdict, note] = process.argv.slice(1);
const rec = { commit, tree, verdict, note, at: new Date().toISOString() };
fs.writeFileSync(".gogogo/verify-local.json", JSON.stringify(rec, null, 2) + "\n");
' "$commit" "$tree" "$verdict" "$note"

echo "recorded: $verdict for $commit"
if [ "$verdict" = "fail" ]; then
	echo "  fix the finding, then run make verify-local again before signoff"
fi
