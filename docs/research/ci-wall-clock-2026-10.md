# CI wall-clock: where the minutes go

Reviewed: 2026-10-29

Kun Chen's argument in [the thread that prompted this
page](https://x.com/kunchenguid/status/2115565245293039814) is that once code
review stops being the bottleneck, CI becomes one: a pipeline that just "runs
all the build, lint, tests" is the next thing agent speed waits on. The three
levers he names are balancing CI against CD, modularizing so only the touched
module is built, and test selection.

This page records which of those apply here, what shipped, and what was
measured instead of assumed.

## The measurement

CI wall-clock per PR was 8.6–12 minutes (six consecutive runs, `gh run view
--json jobs`). Only one job was on the critical path:

| job | time | what it is |
|---|---|---|
| `race` | 620s test + 48s setup | `go test -race` over the whole module |
| `browser` | 265s | build + five client suites, serial |
| `checks` | 232s | a block of cheap Go gates, lint, CSS |
| `prod-image` | 114s | boot the real image |
| `site` | 60s | docs layout in a browser |

Inside `race`, one package was a third of the job and unsplittable by package
parallelism — the suite runs in a single process per package:

| package | time |
|---|---|
| `internal/installer` | 360s |
| `internal/server` | 130s |
| `features/todo/handlers` | 120s |
| `bin` | 79s |
| `internal/demo` | 61s |
| every other package | ~150s total |

The whole suite was 1102s of test time in a 620s job: the machine carries
roughly 1.8x, not 4x, because one package saturates it alone.

## Result

Measured on the same workflow across nine runs. The progression matters more
than any single number, because the caches have to fill before the numbers mean
anything:

| stage | wall-clock |
|---|---|
| before | 8.6–12 min (six runs) |
| first run after the change, every cache cold | 7.6 min |
| caches warm | 4.8–4.9 min |
| cache written by the rejected experiment | 5.9 min |
| **steady state, one cache per matrix leg** | **4.1 min** |

| job | before | after |
|---|---|---|
| `race` (the wall) | 620s test | 168s (installer shard) and 160s (tail) |
| `browser` | 265s | 190–197s (build 76s → 7s) |
| `checks` | 232s | 105–114s (lint 107s → 18s) |
| `prod-image` | 114s | 108–159s |

The wall is now `browser`, which is where the next look goes: it builds the
binary and then runs five client suites serially. That is a deliberate choice
(parallel client suites starve each other's wall-clock waits — see
local-ci.md), so the lever there is per-suite cost, not more concurrency.

## What shipped

**Shard by measured cost, not by count.** `scripts/ci-shard-packages.sh` holds
the measured table above and gives every outlier a shard of its own before
bin-packing the long tail; `race` runs as two shards. The union of all shards
is asserted to be exactly `scripts/web-packages.sh`, by that script's own test.

**Narrow the suite on pull requests only.** The `changes` job resolves the
packages a PR can affect — the touched packages plus their transitive
dependents, test imports included — and the race shards run that list. A merged
commit still runs everything, so a selection error cannot accumulate across
PRs, and an unreadable diff fails open to the full list.

**Cache only what is narrow, and never let a restore gate a build.** An
explicit `actions/cache@v4` step covers the Go build cache and the Playwright
browsers, each `continue-on-error`. The Go *module* cache is deliberately not
cached: measured across the same two runs, restoring it costs 23-33s per job
and saves 16-29s of download — break-even to a net loss, decided by I/O against
a proxy. The build cache is the one that pays, and it pays everywhere the Go
toolchain compiles anything, including `golangci-lint` and `go vet` inside the
trimmed trees.

**One cache per matrix leg.** Both race shards share a job id, so the first key
written was shared and the first leg to finish overwrote the other's half-built
cache: the same code measured 207s of installer test time in one run and 404s in
another. `strategy.job-index` in the key made the installer shard land at 168s
and hold there.

## Rejected, with the evidence

**Re-enabling the actions' internal caches.** `setup-go`, `setup-node` and
`golangci-lint-action` were turned off in commit `d50d0fb` after malformed
restores (`tar` exit 2) aborted jobs. Those stay off. The new steps are a
different mechanism — the explicit action's JS/zstd restore, not the system
`tar` those internal caches used — and `continue-on-error` converts any restore
failure into a cold build instead of a red gate. If a restore ever fails
repeatedly, delete the step: the cost is the seconds it was saving, not
correctness.

**Splitting `race` by package count.** Half the packages on each side leaves
`internal/installer` sharing a shard with thirty others, which is the situation
that produced the 620s job. Measured: the outlier is not parallelized by
package splitting at all.

**Running the installer suite only on master.** The engine tests are the only
proof that a trimmed tree still contains everything; they cover a path every
`gogogo` subcommand goes through, and moving them off the pre-merge gate trades
a real safety net for about a minute. The shard split keeps the gate and moves
the cost.

**Raising the trimmed-tree concurrency.** The suite already bounds how many
repo copies may fork a `go vet ./...` at once. Raising that bound from two to
the core count, on the theory that a warm cache makes each unit cheaper, cost
+176s (207s → 383s test): `go vet` is itself parallel over every core, so two
concurrent copies already oversubscribe four vCPUs and four of them thrash.
The constant two stays.

**Dropping the browser suites from PRs.** They are the only coverage of the
client transport (whiteboard ordering, notes caret, SSE opener) and each one
encodes a regression that shipped. This is the one place the CI/CD rebalance
does not apply.

**`vendor/`.** Hermetic, but every template clone and every scanner pays for it
on each change.

## Revisit triggers

- A `race` shard smaller than its own setup means the split has outgrown the
  suite; drop to one shard.
- A package that becomes a third of any shard belongs in the cost table, not in
  the leftover bin.
- If `internal/installer` stops being the outlier, the cost table is stale:
  refresh it with the command in the script's header before tuning anything.
- If the module-cache restore ever gets cheap (a smaller graph, or a faster
  restore path), re-measure it rather than assuming the 2026-10 verdict holds.

## A flake the split surfaced

The first sharded run failed on three `goleak` assertions in `internal/queue`
that had been passing for months: they called `t.Parallel()` while asserting on
a process-global goroutine count, so a paused sibling test
(`testing.(*testState).waitParallel`) and another test's open transaction were
reported as leaks. Sharding changed the scheduling enough to expose it, not to
cause it. The assertions now run in the serial phase and ignore the paused-test
frame, and a deliberately injected goroutine still turns them red.
