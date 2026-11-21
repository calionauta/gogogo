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

# ── 2. an outlier is isolated, not balanced against the tail ──
# ASSERTED AGAINST AN INJECTED TABLE, NOT THE MEASURED ONE. The mechanism is
# "a package at or above OUTLIER gets a shard to itself while shards remain";
# the seconds are measured data that rots. This case used to hardcode
# "shard 0 of 2 is internal/installer", so when that package fell from 360s to
# 6s the SPLIT was right and this test was red — a test that breaks on a data
# refresh is a test that gets deleted instead of refreshed.
#
# The fixture has to be big enough that balancing WOULD contaminate shard 0:
# the tail must out-sum the outlier, or the greedy packer leaves it alone by
# accident and the case passes with the mechanism switched off (it did — a
# 6-package fixture summed to 253s against a 360s outlier).
heavy="${TMP}/heavy.txt"
cat >"$heavy" <<'EOF'
/repo/internal/installer
/repo/internal/server
/repo/features/todo/handlers
/repo/bin
/repo/internal/demo
/repo/features/notes/handlers
EOF
heavy_costs="${TMP}/heavy-costs.txt"
cat >"$heavy_costs" <<'EOF'
internal/installer 360
internal/server 130
features/todo/handlers 120
bin 79
internal/demo 61
features/notes/handlers 56
EOF
out=$(SHARD_COSTS_FILE="$heavy_costs" SHARD_UNIVERSE_FILE="$heavy" bash "$SCRIPT" 0 2)
case "$out" in
"/repo/internal/installer") ;;
*) fail "with an injected 360s outlier, shard 0 of 2 should be it alone, got: ${out}" ;;
esac
# …and the tail must still be complete, including a package that is NOT an
# outlier. Matched without a pipe: `printf | grep -q` closes the pipe early and
# pipefail turns that into exit 141 (measured — this case was flaky until it was
# rewritten as a case match).
tail_out=$(SHARD_COSTS_FILE="$heavy_costs" SHARD_UNIVERSE_FILE="$heavy" bash "$SCRIPT" 1 2)
case $'\n'"$tail_out"$'\n' in
*$'\n'/repo/internal/server$'\n'*) ;;
*) fail "shard 1 of 2 lost a package that is not an outlier" ;;
esac
# …and the SAME universe with a table where nothing is an outlier must NOT
# reserve a shard: the isolation is driven by the data, not by the name.
flat_costs="${TMP}/flat-costs.txt"
cat >"$flat_costs" <<'EOF'
internal/installer 6
internal/server 6
features/todo/handlers 6
bin 6
internal/demo 6
features/notes/handlers 6
EOF
flat0=$(SHARD_COSTS_FILE="$flat_costs" SHARD_UNIVERSE_FILE="$heavy" bash "$SCRIPT" 0 2 | grep -c . || true)
[ "$flat0" -gt 1 ] ||
	fail "a flat cost table must not isolate any package, but shard 0 held $flat0 package(s)"

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

# ── 6. isolation may not cost more than it saves ──
# WHY: case 2 proves an outlier CAN get a shard to itself; it says nothing about
# whether it SHOULD. The reservation rule gives at most `total - 1` shards to
# outliers, so everything else packs into the LAST one — and when the isolated
# package is cheaper than that packed shard, isolation is a straight loss.
# Measured 2026-11-20 with OUTLIER=60: the real table isolated a 41s package
# while the other 113 summed to ~630s, giving 62s | 172s | 169s. Case 2 was
# blind to it because it only asserts the mechanism, against an injected table.
#
# The property: no shard may carry more than 2x the cheapest shard's load.
# That is what balancing is for, and it needs no real measurement.
#
# The fixture is the shape of today's data: the heaviest package (70s) is NOT
# dominant against a 15s tail — exactly the shape OUTLIER=60 was wrong about.
wide="${TMP}/wide.txt"
for i in $(seq 1 40); do echo "/repo/pkg-${i}"; done >"$wide"
wide_costs="${TMP}/wide-costs.txt"
{
	echo "pkg-1 70"
	echo "pkg-2 65"
	for i in $(seq 3 40); do echo "pkg-${i} 15"; done
} >"$wide_costs"

for total in 2 3; do
	loads=""
	for ((i = 0; i < total; i++)); do
		pkgs=$(SHARD_COSTS_FILE="$wide_costs" SHARD_UNIVERSE_FILE="$wide" \
			bash "$SCRIPT" "$i" "$total" | sed 's|.*/||')
		sum=0
		for p in $pkgs; do
			c=$(awk -v k="$p" '$1 == k { print $2 }' "$wide_costs")
			sum=$((sum + ${c:-1}))
		done
		loads="${loads}${sum} "
	done
	min=$(printf '%s\n' $loads | sort -n | head -1)
	max=$(printf '%s\n' $loads | sort -n | tail -1)
	[ "$min" -gt 0 ] || fail "total=$total: computed an empty shard (loads: $loads)"
	[ "$max" -le $((min * 2)) ] ||
		fail "total=$total: split is lopsided (loads: $loads) — an isolated package is starving the tail"
done

echo "✓ ci-shard-packages: exact partition, outlier isolated, unknown packages kept, empty scope silent, bad args rejected, split balanced"
