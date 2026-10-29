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

Measured on the same workflow across the runs that shipped it. The progression
matters more than any single number, because the caches have to fill before the
numbers mean anything:

| stage | wall-clock |
|---|---|
| before | 8.6–12 min (six master runs) |
| first run after the sharding change, every cache cold | 7.6 min |
| sharded + warm cache | 4.1–4.9 min |
| client suites on two runners, module zips cached | 3.3 min |
| **steady state now** | **3.3 min** |

| job | before | after |
|---|---|---|
| `race` | 620s test, one job | 161s (installer shard), 94s and 134s (tail shards) |
| `changes` (serial prefix on everything) | 60s, 50 of them `go mod download` | 24–31s |
| `browser` | 265s | 122s and 129s on two runners |
| `checks` | 232s | 94–115s (lint 107s → 18s) |
| `prod-image` | 114s | 140–156s |

The wall is now `race shard 0` — `internal/installer`, one package, one process.

A pull request runs on its own merge ref, and a cache is scoped to the ref that
wrote it, so the first PR after this change had nothing to restore even though
the branch's push runs did. Its own rerun restored what the first attempt saved
(3-8s restores) and came back at 3.9 min. After merge, each push to `master`
refreshes the caches PRs restore from, so a PR should not normally pay that cold
run — but the very first one on a fresh branch or a new `go.sum` will, and that
is a cold start rather than a regression.

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

**The module zip cache, not the module tree.** Caching `~/go/pkg/mod` was
break-even to a loss. Caching only `~/go/pkg/mod/cache/download` (a third of the
size) measured 7–8s to restore plus 9s to extract, against 25–50s of cold
download — and it pays twice on the `changes` job, which is a serial prefix on
every other job and was costing the whole run 50s of downloading.

**Two runners for the client suites.** The five suites were 120s serial behind
~80s of setup. Grouped by measured cost (smoke + whiteboard + scroll | notes +
SSE) they land at 122s and 129s. The parallelism is across runners on purpose:
the starvation `ci-fanout.sh` documents is suites sharing one runner's cores.

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

**A buildx layer cache for the production image.** Rejected on measurement, not
on principle: the Dockerfile already layers `go.mod`/`go.sum` first, but
`COPY . .` and the CGO Go build are invalidated by every commit, so the cache
saved nothing (145s with buildx against 150s without) while `setup-buildx`
added 21s to the job. A `--mount=type=cache` would cache the compile itself,
but no buildx cache exporter carries mount contents, so it cannot survive an
ephemeral runner. The plain `docker build` stays.

**Dropping the browser suites from PRs.** They are the only coverage of the
client transport (whiteboard ordering, notes caret, SSE opener) and each one
encodes a regression that shipped. This is the one place the CI/CD rebalance
does not apply.

**`vendor/`.** Hermetic, but every template clone and every scanner pays for it
on each change.

## What is left, with numbers

The wall is now `race shard 0` (161s: `internal/installer`, one package, one
process), then `prod-image` (140s), the tail shards (94s and 134s), and the
client groups (122s and 129s). Everything except the first two is already below
the wall, so more shards there would buy nothing.

**Two more shards for the trimmed-tree units.** Splitting those units by
index across two runners lands each near 90s, which puts the wall at
`prod-image` (140s) and takes the run from 3.3 to about 2.9 min. The cost is a
fourth matrix leg plus a test-side env saying which half a process runs; it is
the only lever left that does not trade coverage, and it is deferred rather than
rejected — at 24s of wall-clock, the extra axis of CI a reader has to hold is
the thing being weighed.

**The production image (140s).** The separation between "build the image" and
"boot the built image" is the gate that caught the 0.44.0 outage, so the lever
is the compile inside it, not the gate. Its Go build runs with a cold cache on
every runner: a buildx cache mount would fix that, and no buildx cache backend
exports mount contents, so it cannot survive an ephemeral runner. Building the
binary on the runner and copying it in would cache the compile and weaken the
gate to "the image layout boots", which is a human decision about what that
test is for.

**A Go-native cache instead of an archive.** Go 1.24 added `GOCACHEPROG`: the
toolchain pipes cache PUT/GETs to a long-running external program instead of
archiving a directory ([Depot's write-up](https://depot.dev/blog/go-remote-cache)).
That removes the tar-flake class this page inherited, and a shared backend makes
the cache cross-branch, which closes the cold-start case a PR hits when its base
branch has nothing cached (measured at 8.1 min on the run that merged the cache
steps, before any push to master had populated them). It can serve the module
cache through the `GOPROXY` protocol too.
Implementations: [`gocica`](https://github.com/gocica-go/gocica) (backed by the
GitHub Actions cache), [`cacheprog`](https://github.com/platacard/cacheprog) and
[`gobuildcache`](https://github.com/richardartoul/gobuildcache) (S3). This one is
a supply-chain decision, not a tuning knob: a new third-party action or a bucket
this repo does not have today, so it wants a human gate before an agent adds it.

**Faster runners.** [CI Bench](https://cibench.com/compare/runson-vs-blacksmith-vs-depot-vs-namespace/)
measured RunsOn at 1.2x and Blacksmith at 1.6x GitHub-hosted — at a 3.3 min
wall that is roughly 2.7–2.9 min, for money and trust in a runner vendor. A
cost decision, not an engineering one.

**Already taken, checked rather than assumed.** `concurrency` with
`cancel-in-progress: true` is in `ci.yml`, so a superseded push does not race the
newer one. A merge queue would not help this shape: one maintainer, PRs that are
already gated, and a queue that would add wait time before the run rather than
shorten it.

## Revisit triggers

- A `race` shard smaller than its own setup means the split has outgrown the
  suite; drop to one shard.
- A package that becomes a third of any shard belongs in the cost table, not in
  the leftover bin.
- If `internal/installer` stops being the outlier, the cost table is stale:
  refresh it with the command in the script's header before tuning anything.
- If the module-cache restore ever gets cheap (a smaller graph, or a faster
  restore path), re-measure it rather than assuming the 2026-10 verdict holds.

## Two flakes the work surfaced

The first sharded run failed on three `goleak` assertions in `internal/queue`
that had been passing for months: they called `t.Parallel()` while asserting on
a process-global goroutine count, so a paused sibling test
(`testing.(*testState).waitParallel`) and another test's open transaction were
reported as leaks. Sharding changed the scheduling enough to expose it, not to
cause it. The assertions now run in the serial phase and ignore the paused-test
frame, and a deliberately injected goroutine still turns them red.

The second is the same shape of latent bug in `internal/auth`: eight security
assertions used a hardcoded `"000000"` as the code that must be refused, and
`verifyTOTP` accepts one step of clock skew either way — so the "wrong" code is
a real code about once in 600,000 window samples (measured over 200k random
secrets). One run hit it (`stepup_test.go:71: wrong code accepted`). The wrong
code is now derived from a window ten steps away and checked against the three
accepted ones, so it cannot collide by construction rather than by luck.
