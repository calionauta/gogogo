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
explicit `actions/cache@v4` step covers the Go build cache, the Go module cache
and the Playwright browsers, each `continue-on-error`.

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
