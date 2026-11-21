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
#   bin/shard-costs.sh --refresh      # re-measure, rewrite the table below
#   bin/shard-costs.sh --estimate     # simulate the split without running CI
# Both are free (they read old run logs). The manual recipe is:
#   gh run view <run-id> --log | grep -E 'ok\s+github\.com/calionauta/gogogo/\S+\s+[0-9.]+s'
# then update the table below. A stale table degrades to a worse split, never
# to a wrong one: unknown packages cost 1s and land in a shared shard, and the
# union of all shards is always exactly web-packages.sh.
#
# MEASURED 2026-11-20 (run 44735623082): the 2026-11-17 table below was stale
# in a direction that did not show up as imbalance — shard 0 ran 62s against
# 172s and 169s for the others. See the STALE TABLE note under OUTLIER.
set -euo pipefail

# OUTLIER: a package at or above this measured cost gets a shard of its own
# (while shards remain), instead of being balanced against the others.
#
# WHY THIS IS BIGGER THAN THE HEAVIEST PACKAGE (and why that is deliberate).
# The reservation rule above stops at `total - 1`, so with 3 shards at most TWO
# packages get a shard each; everything else is greedy-packed into the last one.
# When the isolated package costs LESS than the packed shard it was isolated
# from, isolation is a straight loss: measured 2026-11-20, the one package above
# a 60s threshold (features/todo/handlers, 41s here) got a whole runner while the
# other 114 packages summed to ~630s in a single shard — 62s | 172s | 169s, a
# 110s spread that a balanced split turns into 68s | 68s | 56s. The mechanism is
# for genuine DOMINANCE, so the threshold sits above the heaviest measured
# package: with today's numbers nothing is reserved and the greedy packer wins.
# Lower it only when one package dwarfs the sum of the rest (as internal/installer
# did at 360s of a 620s suite) — then isolating it is worth a runner.
OUTLIER=100

# cost <package-dir-suffix> <seconds> — suffix match, so a moved parent
# directory does not silently drop the entry.
costs() {
	cat <<'EOF'
features/notes/handlers 68
bin 68
features/whiteboard/handlers 53
features/todo/handlers 41
internal/server 40
internal/installer 32
internal/demo 24
features/config/handlers 21
features/account/handlers 19
features/room/handlers 18
features/chat/handlers 18
internal/auth 17
features/review/handlers 17
internal/queue 15
internal/rbac 13
cmd/gogogo-mcp 13
internal/authpages 9
features/todo/service 9
features/notifications/handlers 8
features/dashboard/handlers 8
cmd/web 8
cmd/check-docs 8
internal/capabilities 7
features/graph/handlers 7
internal/llm/fakeserver 6
internal/components 5
features/credits/handlers 5
internal/nats 4
internal/dagnats 4
internal 4
features/license/handlers 4
internal/goakt 3
internal/dbschema 3
internal/dbload 3
internal/collab 3
features/onboarding 3
cmd/check-flatdeps 3
rules 2
internal/natswire 2
features/genui/components 2
features/credits 2
web/resources 1
internal/uiregistry 1
internal/ui 1
internal/themes 1
internal/shape 1
internal/secrets 1
internal/search 1
internal/recipestate 1
internal/recipe/register 1
internal/recipe 1
internal/prompts 1
internal/projectlocales 1
internal/perf 1
internal/observability 1
internal/llm 1
internal/license 1
internal/instance 1
internal/icon 1
internal/i18n 1
internal/hostgate 1
internal/graph 1
internal/events 1
internal/delivery/notify 1
internal/delivery 1
internal/db 1
internal/datastar 1
internal/cloudcfg 1
internal/analytics 1
features/whiteboard/components 1
features/todo/components 1
features/todo 1
features/review/components 1
features/notifications/components 1
features/notes/components 1
features/kiosk/service 1
features/kiosk/handlers 1
features/kiosk/components 1
features/kiosk 1
features/graph/native 1
features/dashboard/components 1
features/config/components 1
features/chat/store 1
features/chat 1
features/authviews 1
features/app 1
config 1
cmd/ui-registry 1
cmd/prompt-eval 1
cmd/check-typed-states 1
cmd/check-test-value 1
cmd/check-skill-frontmatter 1
cmd/check-secrets-env 1
cmd/check-prompts 1
cmd/check-product-boundary 1
cmd/check-domains 1
cmd/check-classes 1
cmd/bump-go-toolchain 1
catalog 1
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