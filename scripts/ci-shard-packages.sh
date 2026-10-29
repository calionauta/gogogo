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
# Measured package wall-times (race, ubuntu-latest 4 vCPU), CI run 40789079197
# on 2026-10-29 — the run that motivated this file:
#
#   internal/installer            360s   <- 58% of the whole job
#   internal/server               130s
#   features/todo/handlers        120s
#   bin                            79s
#   internal/demo                  61s
#   features/notes/handlers        56s
#   features/whiteboard/handlers   42s
#   internal/rbac                  28s
#   (everything else, 60 packages) ~150s
#
# Sum of all 68 packages: 1102s of test time for a 620s job. internal/installer
# is special in two ways: it is a third of the total, and it saturates the vCPUs
# on its own (each unit copies the repo and forks a `go vet ./...`, bounded two
# at a time by trimVetParallel). Balancing it against other packages by summed
# cost just gives it more work to starve on — it gets a shard to itself.
#
# REFRESHING THE TABLE (do this when the race job's shape changes):
#   gh run view <run-id> --log | grep -E '^race.*(ok|FAIL)\s+[^ ]+\s+[0-9.]+s'
# then update the table below. A stale table degrades to a worse split, never
# to a wrong one: unknown packages cost 1s and land in a shared shard, and the
# union of all shards is always exactly web-packages.sh.
set -euo pipefail

# OUTLIER: a package at or above this measured cost gets a shard of its own
# (while shards remain), instead of being balanced against the others.
OUTLIER=300

# cost <package-dir-suffix> <seconds> — suffix match, so a moved parent
# directory does not silently drop the entry.
costs() {
	cat <<'EOF'
internal/installer 360
internal/server 130
features/todo/handlers 120
bin 79
internal/demo 61
features/notes/handlers 56
features/whiteboard/handlers 42
internal/rbac 28
internal/auth 17
features/review/handlers 17
features/chat/handlers 16
features/room/handlers 16
features/account/handlers 16
features/config/handlers 15
internal/queue 12
internal/dagnats 10
internal/llm/fakeserver 9
features/notifications/handlers 8
internal/capabilities 6
rules 5
internal/nats 5
cmd/check-docs 4
internal/goakt 3
features/credits/handlers 3
cmd/web 3
features/landing/handlers 3
EOF
}

if [ "${1:-}" = "--costs" ]; then
	costs
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
' "$OUTLIER" "$index" "$total" "$(costs)" <<<"$all"