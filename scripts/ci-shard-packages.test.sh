#!/usr/bin/env bash
# ci-shard-packages.test.sh — the contract of the race-suite shard split.
#
# Why this exists: the shard split decides WHICH TESTS RUN. A split that is
# merely inefficient costs minutes; a split that drops a package — or hands the
# same package to two shards, or emits a blank line that `go test` reads as "the
# current directory" — silently narrows the gate. That is the failure mode this
# file pins, in the same spirit as assert-public-ready.test.sh: the properties
# are what make the thing usable, and each case can actually go red.
#
# Properties:
#   1. the union of every shard is EXACTLY the universe — no gap, no duplicate;
#   2. the measured outlier (internal/installer) gets a shard to itself, because
#      balancing it against other packages by summed cost is what made the job
#      slow in the first place;
#   3. an unknown package still lands in exactly one shard (the table rots, the
#      partition must not);
#   4. an empty universe produces NO output — not one blank line;
#   5. an out-of-range index/total fails loudly instead of printing a wrong list.
#
# Everything runs against a synthetic universe: no repo, no `go list`, instant.
set -euo pipefail

SCRIPT="$(cd "$(dirname "$0")" && pwd)/ci-shard-packages.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

fail() {
	echo "✗ $1" >&2
	exit 1
}

shards() { # shards <universe-file> <total> → every package, one per line
	local file="$1" total="$2" i
	for ((i = 0; i < total; i++)); do
		SHARD_UNIVERSE_FILE="$file" bash "$SCRIPT" "$i" "$total"
	done
}

# ── 1. union == universe, exactly, at every arity ──
universe="${TMP}/universe.txt"
cat >"$universe" <<'EOF'
/repo/internal/installer
/repo/internal/server
/repo/features/todo/handlers
/repo/features/todo/components
/repo/internal/db
/repo/feature-nobody-measured-yet
EOF

for total in 1 2 3 6; do
	shards "$universe" "$total" | sort >"${TMP}/got.txt"
	sort "$universe" >"${TMP}/want.txt"
	diff -u "${TMP}/want.txt" "${TMP}/got.txt" >&2 ||
		fail "total=$total: shards are not an exact partition of the universe"
	# A duplicate shows up as a shorter union than the sum of shard sizes.
	distinct=$(sort -u "${TMP}/got.txt" | wc -l)
	lines=$(wc -l <"${TMP}/got.txt")
	[ "$distinct" -eq "$lines" ] || fail "total=$total: a package appears in two shards"
done

# ── 2. the measured outlier is isolated, not balanced ──
# The fixture has to be big enough that balancing WOULD contaminate shard 0:
# the non-installer side must out-sum the outlier, or the greedy packer leaves
# shard 0 alone by accident and this case passes with the mechanism switched off
# (it did — a 6-package fixture summed to 253s against the outlier's 360s).
heavy="${TMP}/heavy.txt"
cat >"$heavy" <<'EOF'
/repo/internal/installer
/repo/internal/server
/repo/features/todo/handlers
/repo/bin
/repo/internal/demo
/repo/features/notes/handlers
EOF
out=$(SHARD_UNIVERSE_FILE="$heavy" bash "$SCRIPT" 0 2)
case "$out" in
"/repo/internal/installer") ;;
*) fail "shard 0 of 2 should be internal/installer alone, got: ${out}" ;;
esac
# …and the tail must still be complete, including a package that is NOT an
# outlier. Matched without a pipe: `printf | grep -q` closes the pipe early and
# pipefail turns that into exit 141 (measured — this case was flaky until it was
# rewritten as a case match).
tail_out=$(SHARD_UNIVERSE_FILE="$heavy" bash "$SCRIPT" 1 2)
case $'\n'"$tail_out"$'\n' in
*$'\n'/repo/internal/server$'\n'*) ;;
*) fail "shard 1 of 2 lost a package that is not an outlier" ;;
esac

# ── 3. an unknown package lands in exactly one shard ──
# The cost table is measured data and will rot; the partition may not.
count=$(shards "$universe" 3 | grep -cx "/repo/feature-nobody-measured-yet" || true)
[ "$count" -eq 1 ] || fail "unmeasured package appears $count times (want exactly 1)"

# ── 4. empty universe → no output at all ──
# Asserted on BYTES, not on a captured variable: `$( )` strips trailing
# newlines, so a shard that printed one blank line looked identical to one that
# printed nothing — the check was blind to exactly the bug it exists for (found
# by mutating `if result:` to `if True:`; the mutation survived).
: >"${TMP}/empty.txt"
for i in 0 1; do
	SHARD_UNIVERSE_FILE="${TMP}/empty.txt" bash "$SCRIPT" "$i" 2 >"${TMP}/out.txt"
	[ -s "${TMP}/out.txt" ] &&
		fail "empty universe, shard $i: expected zero bytes, got $(wc -c <"${TMP}/out.txt") ('$(cat "${TMP}/out.txt" | head -1)')"
done

# ── 5. bad arguments fail loudly ──
for args in "2 2" "0 0" "-1 2" "0 x"; do
	# shellcheck disable=SC2086 # args is deliberately word-split
	if SHARD_UNIVERSE_FILE="$universe" bash "$SCRIPT" $args >/dev/null 2>&1; then
		fail "args '$args' should have failed"
	fi
done

echo "✓ ci-shard-packages: exact partition, outlier isolated, unknown packages kept, empty scope silent, bad args rejected"
