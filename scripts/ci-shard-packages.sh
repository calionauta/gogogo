#!/usr/bin/env bash
# ci-shard-packages — prints the package directories for one shard of the race
# suite, so the 68-package `go test -race` run is not a single 10-minute job.
#
# Usage:
#   bash scripts/ci-shard-packages.sh <index> <total>   # e.g. 0 2
#   bash scripts/ci-shard-packages.sh --costs           # print the table
#
# WHY A COST TABLE INSTEAD OF A ROUND-ROBIN SPLIT
# A shard is a runner, and a runner has 4 vCPUs. Go already parallelizes across
# packages inside a shard, so the cost of a shard is NOT its package count: it
# is roughly max(slowest package, sum/4). Splitting 68 packages in half by
# count puts internal/installer next to 33 other packages and changes nothing
# worth measuring. The outlier has to be isolated.
#
# Measured package wall-times (race, ubuntu-latest 4 vCPU), re-measured
# 2026-11-17 from CI run 44142988832 and confirmed across 8 consecutive runs
# (44133461426…44165959359):
#
#   features/todo/handlers        64s
#   bin                           53s
#   features/whiteboard/handlers  50s
#   features/notes/handlers       49s
#   internal/server               36s
#   internal/demo                 17s
#   ... long tail ...
#   internal/installer             6s
#
# WHY internal/installer IS NO LONGER THE OUTLIER. The 2026-10-29 table gave it
# 360s (58% of the job) because each unit copied the repo and forked
# `go vet ./...`. The fixture was then narrowed, and the package now measures
# 6s in every run of the last 8 — a 60x drop. A stale cost table is not a
# correctness bug (the union of shards is still exactly web-packages.sh), it is
# an efficiency bug with a measured shape: with `internal/installer 360` the
# greedy packer reserves shard 0 for a 6s package and the theoretical makespan
# is 360s; with the real numbers it is 182s and the three shards finish within
# 14s of each other. Re-measure before editing this table.
#
# REFRESHING THE TABLE (do this when the race job's shape changes):
#   gh run view <run-id> --log | grep -E '^race.*(ok|FAIL)\s+[^ ]+\s+[0-9.]+s'
# then update the table below. A stale table degrades to a worse split, never
# to a wrong one: unknown packages cost 1s and land in a shared shard, and the
# union of all shards is always exactly web-packages.sh.
set -euo pipefail

# OUTLIER: a package at or above this measured cost gets a shard of its own
# (while shards remain), instead of being balanced against the others.
# 60, not 300: the heaviest package now measures 64s, so "a shard to itself"
# costs a runner and saves nothing unless the package is genuinely dominant.
# Isolating a 64s package from a 50s one buys 14s; isolating it from 6s packages
# buys nothing and starves the tail. Keep this at the point where a package is
# worth more than the packing it distorts.
OUTLIER=60

# cost <package-dir-suffix> <seconds> — suffix match, so a moved parent
# directory does not silently drop the entry.
costs() {
	cat <<'EOF'
features/todo/handlers 64
bin 53
features/whiteboard/handlers 50
features/notes/handlers 49
internal/server 36
internal/demo 17
features/config/handlers 17
features/room/handlers 16
features/account/handlers 16
features/chat/handlers 15
features/review/handlers 13
internal/queue 12
internal/auth 10
internal/authpages 10
internal/dagnats 9
internal/rbac 9
cmd/check-docs 9
features/graph/handlers 8
internal/capabilities 8
features/todo/service 8
features/dashboard/handlers 8
features/notifications/handlers 7
internal/llm/fakeserver 6
internal/installer 6
internal/nats 5
internal/goakt 3
features/credits/handlers 3
cmd/web 3
features/landing/handlers 3
EOF
}

cost_table() {
	if [ -n "${SHARD_COSTS_FILE:-}" ]; then
		grep -v '^[[:space:]]*$' "$SHARD_COSTS_FILE"
	else
		costs
	fi
}

if [ "${1:-}" = "--costs" ]; then
	cost_table
	exit 0
fi

index=${1:?usage: ci-shard-packages.sh <index> <total>}
total=${2:?usage: ci-shard-packages.sh <index> <total>}

if ! [ "$total" -ge 1 ] 2>/dev/null; then
	echo "ci-shard-packages: total must be a positive integer, got '$total'" >&2
	exit 2
fi
if ! [ "$index" -ge 0 ] 2>/dev/null || [ "$index" -ge "$total" ]; then
	echo "ci-shard-packages: index must be 0..$((total - 1)), got '$index'" >&2
	exit 2
fi

# The universe to shard. Defaults to the full list from the same source of truth
# every other Go gate uses. SHARD_UNIVERSE_FILE lets test selection (P2) narrow
# the universe first: sharding composes with selection instead of overriding it,
# so a PR that touches two packages still gets them split across shards rather
# than paying for all 80.
if [ -n "${SHARD_UNIVERSE_FILE:-}" ]; then
	all=$(grep -v '^[[:space:]]*$' "$SHARD_UNIVERSE_FILE" || true)
else
	all=$(bash "$(dirname "$0")/web-packages.sh")
fi

# total == 1 is the unsharded run: print everything and stay out of the way.
# An empty universe (test selection found no Go package affected) prints nothing
# and exits 0: the caller skips the run instead of testing the current dir.
if [ "$total" -eq 1 ]; then
	[ -n "$all" ] && printf '%s\n' "$all"
	exit 0
fi

# The cost table arrives as an ARGUMENT, not on stdin: python's stdin is the
# package list (`<<<`), so piping `costs` into it leaves the writer connected to
# a pipe nobody reads — it loses the race and pipefail surfaces a 141 (measured:
# this made the script fail 4 runs in 10 before the test found it).
#
# SHARD_COSTS_FILE overrides the table. It exists so the CONTRACT TEST can
# assert the mechanism (an outlier gets a shard to itself) without asserting the
# MEASURED DATA, which rots: the test used to hardcode "shard 0 of 2 is
# internal/installer", and when that package dropped from 360s to 6s the split
# was correct while the test was red. A test that fails when the data changes is
# a test that gets deleted.
python3 -c '
import sys

outlier_threshold = int(sys.argv[1])
index = int(sys.argv[2])
total = int(sys.argv[3])
packages = [line.rstrip("\n") for line in sys.stdin if line.strip()]
table = dict(costs_line.split() for costs_line in sys.argv[4].splitlines())

def cost_for(path):
    hit = 0
    for suffix, seconds in table.items():
        if path.endswith("/" + suffix) or path.rsplit("/", 1)[-1] == suffix:
            hit = max(hit, int(seconds))
    return hit or 1

costed = sorted(((cost_for(p), p) for p in packages), key=lambda pair: (-pair[0], pair[1]))

# Shards 0..k-1 are reserved, one each, for the outliers (heaviest first), but
# never at the cost of leaving no shard for the long tail: with total=2 the
# single outlier gets shard 0 and everything else lands in shard 1.
k = 0
reserved = []
for cost, path in costed:
    if cost < outlier_threshold or k >= total - 1:
        break
    reserved.append((cost, path))
    k += 1

assigned = [list() for _ in range(total)]
load = [0] * total
for shard, (cost, path) in enumerate(reserved):
    assigned[shard].append(path)
    load[shard] = cost

# Greedy longest-first over the remaining shards only.
for cost, path in costed:
    if any(path == p for _, p in reserved):
        continue
    target = min(range(k, total), key=lambda s: (load[s], s))
    assigned[target].append(path)
    load[target] += cost

# An empty shard prints nothing at all. A single blank line would reach the
# caller as one empty package path, and `go test <blank>` is the current dir.
result = "\n".join(sorted(assigned[index]))
if result:
    print(result)
' "$OUTLIER" "$index" "$total" "$(cost_table)" <<<"$all"