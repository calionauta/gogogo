# Moving CI off GitHub: what it would and would not buy

Reviewed: 2026-10-31

The question this page answers: could running CI somewhere else — CircleCI,
Dagger, Woodpecker, self-hosted — make this repo's pipeline **materially faster
and still be free**?

Short answer: **no, and not for the reason people usually expect.** The repo is
public, so the minutes are already unlimited and free; the wall is 198s; and
**the machine is not what makes it 198s.** Measured below. A migration would
cost weeks and could plausibly make it *slower*.

## The three measurements that decide this

These come from `gh api .../runs/<id>/jobs` (queue time = `started_at -
created_at`) and from local race-suite runs, not from vendor claims.

| Measurement | Number | What it rules out |
|---|---|---|
| **Billing exposure** | repo is **public** → standard GitHub-hosted runners are **free and unlimited** ([GitHub docs](https://docs.github.com/en/actions/reference/runners/github-hosted-runners)) | "we'd hit the free-tier cap" — there is no minute cap. 142 runs in 3 days is irrelevant to cost. |
| **Runner queue time** | **max 7-12s per job**, 26-37s aggregate across 11 jobs that overlap → **~12s of wall** | The Boardy post's #1 lever ("up to 11 min waiting for a machine → 8s"). Ours is already 12s. Nothing to reclaim. |
| **Parallelism scaling** | shard 1: **145s serial** (`-p 1`) vs **66s parallel** (10 cores), total CPU 84-109s | Bigger/faster boxes. 2.2x× more cores bought 2.2x, so the shard saturates; but the **slowest single package is 42s** (`features/notes/handlers`), and no machine makes a package finish sooner. |

The third line is the whole argument. `go test` already parallelizes across
packages inside a shard. A shard's floor is `max(slowest package, total CPU /
cores)`. With the slowest package at 42s and the shard sum around 100s, **4
cores is already close to the knee** — moving to 16 cores would shave the
parallel part and leave the 42s tail standing. That is the Amdahl bound the
repo already documented in `ci-wall-clock-2026-10.md`.

And the wall-clock reality from the same period: **three jobs finish within 5s
of each other** (`browser group 1` 154s, `prod-image` 155s, `race shard 0`
196s). Speeding up any one of them by 20s buys **zero** wall seconds.

## The options, against those numbers

### CircleCI — the only one with a real "faster, free" story, and it still loses

Facts: Free plan is 30,000 credits/month; **open-source projects get 400,000
credits/month for Linux/Arm/Docker** ([CircleCI docs](https://circleci.com/docs/guides/plans-pricing/credits/)).
At roughly 10 credits/minute for a 2-core Docker medium that is ~40,000
minutes — far more than our ~1,500 min/month. So **capacity is not the problem**.

What it would actually change: a different resource class *might* give more
cores per job. Our data says more cores buys maybe 20-30s on `race shard 0`
(half of 145→66 is already banked; the tail is 42s) — and **zero** wall, because
two other jobs tie with it. Against that: re-authoring 9 workflows, 33 lefthook
gates, the artifact handoff, `release-please`, `pages`, and the deploy job into
CircleCI syntax and orbs.

**Verdict: rejected.** Pays migration cost for wall-clock it cannot deliver.

### Dagger — a build-graph engine, not a speed fix

Dagger is not a runner; it is a programmable DAG with content-addressed caching.
It can genuinely transform pipelines where the *same* steps re-run constantly.
But our expensive step is `go test -race`, whose cache is already warm and
already keyed correctly by the Go toolchain. Wrapping `go test` in Dagger adds
a container build and an engine round-trip around the one thing already cached.

The Docker/CGO layers do repeat (`prod-image`, 155s), and there Dagger's local
cache could help — but `scripts/smoke-prod-image.sh` already measured that
BuildKit layer caching saves nothing because `COPY . .` plus the CGO build
invalidate every commit. Dagger would hit the same invalidation.

**Verdict: rejected for wall-clock.** Worth revisiting only if we ever have
many interdependent artifacts and slow multi-stage builds.

### Woodpecker — free as in self-hosted, expensive as in *not free*

Woodpecker is a ~130 MB control plane (100 MB server + 30 MB agent) and needs a
host plus a Docker/K8s backend ([Woodpecker docs](https://woodpecker-ci.org/docs/administration/general)).
It is free software, but "free CI" then means *you now own the runner host*.

Here that is a real cost, not a hypothetical: the shared server is an 8 GB box
already running bb + production, and the repo's own `AGENTS.md` forbids a
whole-repo lint or full test suite there because it drives the box into swap.
Adding CI agents would do exactly that. The alternative — a free VM — is
documented in `docs/research/ci-wall-clock-2026-10.md` and costs hours of
tuning for a wall the machine does not control.

**Security footnote that kills the obvious shortcut:** GitHub explicitly
recommends self-hosted runners **only for private repositories**, because a fork
of a public repo can execute arbitrary code on the runner by opening a PR
([GitHub docs](https://docs.github.com/en/actions/how-tos/manage-runners/self-hosted-runners/add-runners)).
This repo is public. Runners carrying a deploy key would be reachable by any
fork's PR. That is not a risk worth trading for ~0s of wall.

**Verdict: rejected.** Self-hosting here buys one `slog` line of latency
control for a standing security surface and a shared-host swap risk.

### Self-hosted GitHub runner on a free VM — the only "free and faster" candidate

Free-tier ARM VMs exist (Oracle's always-free Ampere tier is the usual one:
more cores and RAM than `ubuntu-latest`). A self-hosted runner on one would give
more cores per job **and** persistent caches. This is the only option that could
plausibly win on paper.

It still fails the three measurements: more cores cannot beat the 42s package
tail, and the 12s queue is already noise. It also inherits the public-fork
security problem above unless the runner is isolated and never trusts PR code.

**Verdict: deferred, conditionally.** The trigger is written below.

## What *would* actually make this pipeline faster

All free, all already in the repo's own research, none requiring a CI move:

1. **Two-phase sharding** — run each shard's outlier package (`internal/installer`
   today, `features/notes/handlers` at 42s next) as its own job so the tail
   overlaps the rest. `ci-wall-clock-2026-10.md` lists this as the remaining
   levers; it is the only thing that attacks the real bound.
2. **Attack the 42s package, not the machine.** Most of it is SSE stream tests
   with heartbeat waits. That is a test-design cost, and it is the same on any
   provider.
3. **`prod-image` shares the tie** — if phase-1 sharding lands, this becomes the
   critical path and is worth a fresh look.

## Revisit triggers

- **Queue time over ~60s** in `gh api .../jobs` → self-hosted/free-VM runner
  becomes worth costing. Today it is 12s.
- **The slowest single package stays over ~90s** after phase-1 sharding → the
  bound is a test, not a machine; go fix the test.
- **The repo goes private** → GitHub minutes stop being free, and CircleCI's
  open-source credit pool stops applying too; re-run this comparison with a
  cost column. Today it is public.
- **A CI provider ships free per-package-result caching keyed on file hashes**
  (so a shard skips the unchanged tail). That would beat all of the above
  because it attacks the tail rather than the throughput.
