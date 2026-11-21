#!/usr/bin/env bash
# shard-costs — re-measure and refresh the race suite's cost table.
#
# Why this exists. scripts/ci-shard-packages.sh carries a table of measured
# per-package wall-times, and every entry in it is data that rots. The script
# itself records what that costs: the 2026-10-29 table gave internal/installer
# 360s (58% of the suite) and it was measuring 6s — a 60x error that nobody
# noticed until the numbers were re-taken by hand. A comment asking the next
# person to "re-measure before editing this table" is not a mechanism.
#
# This is the mechanism. It reads an existing run's log (free — no CI is
# dispatched) and either prints the table it would write or rewrites it in place.
#
# Usage:
#   bin/shard-costs.sh                        # latest successful ci.yml run
#   bin/shard-costs.sh <run-id>                # a specific run
#
# There is deliberately no --estimate mode. Predicting a shard's wall time needs
# the per-package durations AND the runner's core count; the first attempt at it
# disagreed with a real run by 3x (predicted 228s where shard 0 measured 62s).
# A tool that prints a confident wrong number is worse than no tool. The table it
# writes is measured data, and the split is verified by
# scripts/ci-shard-packages.test.sh instead.
#   bin/shard-costs.sh --refresh [<run-id>]    # rewrite the table in the script
#
# Needs gh (authenticated) and a network to fetch the log. Never writes without
# --refresh.
set -uo pipefail
cd "$(dirname "$0")/.."

SCRIPT=scripts/ci-shard-packages.sh
RUN=""
MODE=print
LOG=""

while [ $# -gt 0 ]; do
	arg="$1"
	shift || true
	case "$arg" in
	--refresh) MODE=refresh ;;
	--log) LOG="${1:-}"; shift || true ;;
	--log=*) LOG="${arg#--log=}" ;;
	-h | --help)
		sed -n '2,24p' "$0" | sed 's/^# \{0,1\}//'
		exit 0
		;;
	'' | *[!0-9]*)
		echo "shard-costs: unknown argument: $arg" >&2
		exit 2
		;;
	*) RUN="$arg" ;;
	esac
done

if [ -z "$LOG" ] && ! command -v gh >/dev/null 2>&1; then
	echo "shard-costs: unavailable (gh not installed)"
	exit 0
fi
# `gh auth status` exits non-zero when a token is present but cannot be
# validated, and in CI that token is the only credential there is, so this gate
# reported "not authenticated" on the one environment it runs in. Presence is
# the precondition; the api call is the real test and reports its own failure.
if [ -z "$LOG" ] && [ -z "${GH_TOKEN:-}" ] && [ -z "${GITHUB_TOKEN:-}" ]; then
	if ! gh auth status >/dev/null 2>&1; then
		echo "shard-costs: unavailable (gh not authenticated)"
		exit 0
	fi
fi

# Without a run id, resolve the newest successful ci.yml run. --log short-circuits
# this: the whole point of passing a log is to parse it without touching gh, and
# an offline caller must not be made to fail on a run lookup it never needed.
if [ -z "$LOG" ] && [ -z "$RUN" ]; then
	RUN=$(gh run list --workflow=ci.yml --limit 20 --json databaseId,conclusion \
		-q '[.[] | select(.conclusion=="success")][0].databaseId' 2>/dev/null || true)
	if [ -z "$RUN" ] || [ "$RUN" = "null" ]; then
		echo "shard-costs: no successful ci.yml run found" >&2
		exit 1
	fi
fi

# `ok  <pkg>  <d>s` lines come from `go test -race`, one per package. A package
# that ran in more than one shard is impossible (the split is a partition) but a
# retried run can repeat one, so take the max rather than the last.
raw=$(
	if [ -n "$LOG" ]; then
		cat "$LOG"
	else
		gh run view "$RUN" --log 2>/dev/null
	fi |
	grep -oE 'ok[[:space:]]+github\.com/calionauta/gogogo/[^[:space:]]+[[:space:]]+[0-9.]+s' |
	sed -E 's|^ok[[:space:]]+github\.com/calionauta/gogogo/||; s|[[:space:]]+([0-9.]+)s$|\t\1|')

if [ -z "$raw" ]; then
	echo "shard-costs: run $RUN has no package timings (not a Go test run?)" >&2
	exit 1
fi

# Seconds are rounded UP so a package never measures as free; below 1s the table
# is meaningless anyway (the packer treats unknown packages as 1s).
table=$(printf '%s\n' "$raw" | awk -F'\t' '
	{ v = $2 + 0; if (v > m[$1]) m[$1] = v }
	END { for (p in m) printf "%s %.0f\n", p, (m[p] < 1 ? 1 : m[p]) }
' | sort -k2 -rn)

count=$(printf '%s\n' "$table" | grep -c .)

case "$MODE" in
print)
	echo "# $count packages, measured from ci.yml run $RUN"
	printf '%s\n' "$table"
	;;
refresh)
	# Replace only the here-doc body of costs(); the surrounding prose stays,
	# because it is the argument for WHY the table exists.
	# The table goes in as an argument, never as stdin: this branch already uses
	# a here-doc to carry the program itself, and two stdin streams on one
	# command is a bug that fails silently on the first line of data.
	SHARD_TABLE="$table" python3 - "$SCRIPT" "$RUN" "$count" <<'PY'
import os, re, sys
path, run, count = sys.argv[1], sys.argv[2], sys.argv[3]
table = os.environ["SHARD_TABLE"].rstrip("\n")
src = open(path).read()
pat = re.compile(r"(costs\(\) \{\n\tcat <<'EOF'\n)(?:.*?\n)(EOF\n\})", re.S)
m = pat.search(src)
assert m, "could not find the costs() here-doc in " + path
# Keep group 1 (the `costs() {` + here-doc opener) and group 2 (`EOF` + `}`),
# and put the new table BETWEEN them. Slicing at m.start(2) would insert the
# table while keeping the old one above it — the first version did exactly that
# and doubled the table.
src = src[:m.end(1)] + table + "\n" + src[m.start(2):]
open(path, "w").write(src)
print("shard-costs: rewrote the table in %s from run %s (%s packages)" % (path, run, count))
PY
	;;
esac
