#!/usr/bin/env bash
# Contract test for bin/shard-costs.sh.
#
# The script exists because the race suite's cost table rots: it is measured by
# hand, and the numbers in it decide which packages share a shard, so a stale
# table silently unbalances the suite (internal/installer was listed at 360s
# when it measured 31s, which reserved an entire shard for one package).
# The script re-derives the table from a real workflow run log instead.
#
# What this pins, and only what the script actually does:
#   1. it is executable — CI calls it directly, and a 644 file is a silent
#      "command not found" the first time anyone invokes it without `bash`;
#   2. it parses `ok <pkg> <d>s` lines into `<name> <cost>` — the table's own
#      format, not a re-ordered one;
#   3. the HIGHEST observed time per package wins, because a retried run can
#      repeat a package and the worst case is what sizes a shard;
#   4. --refresh is idempotent: two runs produce the same bytes, or every run
#      rewrites the table with rounding jitter and shows up as a diff;
#   5. an unusable log does not blank the table.
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
tool="${here}/shard-costs.sh"
real="${here}/../scripts/ci-shard-packages.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fails=0
ok() { printf 'ok    %s\n' "$1"; }
bad() { printf 'FAIL  %s\n        %s\n' "$1" "$2"; fails=$((fails + 1)); }

if [ ! -x "$tool" ]; then
	bad "the script is executable" "mode is $(ls -l "$tool" | cut -d' ' -f1); CI calls it directly"
else
	ok "the script is executable"
fi

# Two samples of the same package at different times, plus a nested command
# package — exactly the shape a sharded race run produces.
cat >"$tmp/log" <<'LOG'
race shard 0	ok  	github.com/calionauta/gogogo/internal/server	40.100s
race shard 1	ok  	github.com/calionauta/gogogo/internal/server	60.700s
race shard 0	ok  	github.com/calionauta/gogogo/features/todo/handlers	12.000s
race shard 1	ok  	github.com/calionauta/gogogo/cmd/web	7.890s
LOG

out="$(bash "$tool" --log "$tmp/log" 2>&1)"
if [ $? -ne 0 ]; then
	bad "--log parses without gh" "$(head -2 <<<"$out")"
else
	ok "--log parses without gh"
fi

case "$out" in
*"internal/server 61"*) ok "the highest observed time per package wins" ;;
*) bad "a package tested twice did not keep its slowest time" "$(printf '%s' "$out" | tr '\n' ' ')" ;;
esac
case "$out" in
*"internal/server 40"*) bad "the faster sample survived" "40s must not beat 60s" ;;
*) ok "the faster duplicate does not win" ;;
esac
case "$out" in
*"cmd/web 8"*) ok "a nested command package is extracted" ;;
*) bad "cmd/web was not extracted" "$(printf '%s' "$out" | tr '\n' ' ')" ;;
esac

# The table is `<name> <cost>`. `cost <name>` is what the first version wrote and
# it produced a table scripts/ci-shard-packages.sh could not read at all.
if printf '%s\n' "$out" | grep -qE '^[a-z][^ ]* [0-9]+$'; then
	ok "the table is emitted as <name> <cost>"
else
	bad "the emitted table is not <name> <cost>" "$(printf '%s\n' "$out" | grep -v '^#' | head -2)"
fi

# --refresh against a scratch copy of the shard script, so the repo's real table
# is never touched by this test.
scratch="$tmp/repo"
mkdir -p "$scratch/scripts" "$scratch/bin"
cp "$real" "$scratch/scripts/ci-shard-packages.sh"
cp "$tool" "$scratch/bin/shard-costs.sh"

refresh() { (cd "$scratch" && bash bin/shard-costs.sh "$@" >/dev/null 2>&1); }

refresh --refresh --log "$tmp/log"
sum1="$(cksum <"$scratch/scripts/ci-shard-packages.sh")"
refresh --refresh --log "$tmp/log"
sum2="$(cksum <"$scratch/scripts/ci-shard-packages.sh")"
if [ "$sum1" = "$sum2" ]; then
	ok "--refresh is idempotent (same log, same bytes)"
else
	bad "--refresh rewrote the table differently on an identical log" "$sum1 vs $sum2"
fi
if grep -q 'internal/server 61' "$scratch/scripts/ci-shard-packages.sh"; then
	ok "--refresh wrote the measured cost into the table"
else
	bad "--refresh did not write the measured cost" "$(grep -c . "$scratch/scripts/ci-shard-packages.sh") lines"
fi

# A log with nothing recognizable must leave the table alone rather than
# truncating it to an empty table the shard script would then split evenly.
before="$(grep -c . "$scratch/scripts/ci-shard-packages.sh")"
printf 'nothing useful here\n' >"$tmp/empty.log"
refresh --refresh --log "$tmp/empty.log"
after="$(grep -c . "$scratch/scripts/ci-shard-packages.sh")"
if [ "$before" = "$after" ]; then
	ok "an unusable log leaves the table untouched"
else
	bad "an unusable log rewrote the table" "$before -> $after lines"
fi

if [ "$fails" -ne 0 ]; then
	printf '\n%d contract failure(s)\n' "$fails"
	exit 1
fi
echo "✅ shard-costs contract holds"
