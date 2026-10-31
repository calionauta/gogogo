# CI speed from the Boardy playbook: what transfers here

Reviewed: 2026-10-30

[Owen Gretzinger's "How I made our CI 20x faster"](https://x.com/owengretzinger/status/2116291924219060319)
reports a PR going from 40 minutes to 2 at Boardy, at a bill that went from $230
to $4,300/month. The number that travels is not "20x": it is *which* of his nine
levers a TypeScript monorepo needed and a Go single-module template already has.

This page is the filter. The constant is the constraint from
[ci-wall-clock](ci-wall-clock-2026-10.md): the wall is 3.3 minutes, `race shard
0` (`internal/installer`, one package, one process), then `prod-image`. Owen's
before-picture is a 40-minute wall where the *machine* was 11 minutes of it.
Applying his list wholesale here would be optimizing a pipeline that already
shipped eight of the nine.

Open source and free is a hard requirement on every row, so his headline lever
is out of scope by construction and the rest are judged on whether they buy
wall-clock at this repo's shape.

## His nine levers, against this repo

| # | Owen's change | status here | why |
|---|---|---|---|
| 1 | Non-GitHub runners (Blacksmith) | **rejected** | Paid vendor, the opposite of the constraint. `ci-wall-clock` already costed it: 1.2-1.6x for money and trust in a runner vendor, on a 3.3 min wall. GitHub-hosted queueing was his 11-minute problem; it is not ours. |
| 2 | `oxlint` instead of ESLint | **n/a** | Go. `golangci-lint` v2.14.0 is the linter and it is Go-native. |
| 3 | Faster typechecker (`bun check`) | **n/a** | Go's compiler is the typechecker; there is no separate tool to swap. |
| 4 | Shard tests onto bigger machines | **shipped, differently** | Three shards (0 = the outlier, 1-2 bin-packed), which is the same conclusion from the same evidence: his "bigger machines mattered more than more shards" is the `race` job's comment about cost being `max(slowest package, sum/cores)`. Bigger runners are lever 1 (paid); the split is free and already in. |
| 5 | `isolate: false` (share a worker per file) | **n/a, and refused in spirit** | That is Vitest reusing a process across files. `go test` already runs many packages per binary and Go tests in one process per package by design. The template's own rule points the other way: packages share state only through `TestMain`, because a fixture that writes a package global is a latent race the moment two tests overlap. |
| 6 | Fix setup (cache, blobless clone, parallelism) | **mostly shipped; two live items** | Go build + module caches are explicit `actions/cache@v4` steps with `restore-keys`; `concurrency: cancel-in-progress` is in. See "The two that do transfer" for the clone and the cache-shape gap. |
| 7 | Run everything in parallel | **shipped** | `race`, `browser`, `checks`, `prod-image`, `site` are independent jobs off one `changes` prefix. His follow-on ("look at what else your slowest job is doing") is exactly what the `internal/installer` split in `ci-wall-clock` did. |
| 8 | Only run what a PR touches, and **test the skip logic** | **shipped** | `changes` classifies docs-only and resolves the affected packages transitively, and both decisions now have contract tests (`scripts/ci-changes-classifier.test.sh`, `scripts/ci-shard-packages.test.sh`) behind `make check-ci-scripts` — see below. |
| 9 | Pre-push hook instead of a line in AGENTS.md | **shipped** | lefthook tiers T1-T5 (`docs/local-ci.md`), and the reason he gives ("agents skip steps") is the reason they are git hooks and not prose. |

## The two that do transfer

### 1. The docs-only classifier was the one untested skip — FIXED

Owen's sharpest line is "Then test the skip logic itself. We hid a server change
from Nx on purpose and all six shards skipped their tests and reported green."
The same class existed here and is now closed.

`changes` decided `docs_only` with an inline regex over `git diff --name-only`
(`^docs/|^site/|\.md$|^LICENSE$|^\.stelow/|^\.gitignore$`) and failed open when
the base was unreadable. A docs-only verdict skips `checks`, `race`, `browser`
AND `prod-image` — verified against the four `needs.changes.outputs.docs_only`
gates in `ci.yml`, not inferred. The *shard selector* next to it has a synthetic
test; the classifier had none, and 40 lines of inline YAML is unreachable from
`go test`.

Applied, in four parts:

- `scripts/ci-changes-classifier.sh` — the path policy as a pure function over
  newline-separated paths on stdin. The workflow keeps the git-dependent part
  (resolving the base, failing open when it cannot be read) where it belongs.
- `scripts/ci-changes-classifier.test.sh` — 18 cases across three groups. The
  "must be false" group is the important one: Go source, `src/css/input.css`, a
  migration, a `.templ`, the CI workflow itself, an unknown top-level dir, and a
  mixed prose+Go changeset. Prose inside a code directory stays inert (it is a
  `.md`; only the extension matters).
- `make check-ci-scripts` — runs this and the existing shard-split contract in
  one target, wired into both `ci-local-fast` and `ci-local`, and as a step in
  the `checks` job. Milliseconds, no git, no repo.
- Verified for behavioural drift before switching: replayed the old inline
  grep and the new script over real commit ranges and confirmed identical
  verdicts on every docs-only commit in the last 40. The change is a
  reorganisation, not a policy change.

RED-PROOF: broadening the policy to treat `.go` as inert fails 7 cases;
short-circuiting the loop (fail open — the dangerous direction) fails all 6
"must be false" cases. Both directions are caught, so the test is not merely
agreeing with the implementation.

The invariant now stated in the script: the policy is an allowlist of INERT
trees, so an unknown top-level directory is impactful until someone allowlists
it. A false "false" costs a run; a false "true" costs a shipped break.

### 2. Superficial clones: measured, not worth it

The `changes` job needs `fetch-depth: 0` (it diffs `base...HEAD`). The other six
checkout sites take the default depth-1 clone. Owen's finding — "shallow breaks
anything that reads git history; use `filter: blob:none`" — does not transfer:
depth-1 is already a fraction of his shallow case, no CI job reads history
outside `changes`, and the six checkouts measure 2-3s each in the job breakdown
(checked in run 41194803602). Not a lever at 2-3s against a 198s wall.


## Measured this round, and left alone

Two levers were re-examined with numbers rather than argument, and both are
already at their local optimum:

- **The `changes` prefix is 30s, and ~18s of it is Go setup.** `changes` needs
  `go list` (for the reverse-dependency closure), so the toolchain plus the
  module cache is not removable. Step breakdown from run 41194803602: checkout
  3s, toolchain 2s, cache restore 7s, warm 9s, classify 2s. Every heavy job waits
  on it, so it is the one cost paid by every run — but the parts that could go
  (`go mod download all`) are exactly what the closure needs.
- **`go mod download all` doubles the cache for nothing** (measured locally on a
  cold `GOMODCACHE`: own graph 1.5 GB / ~45s, `all` 3.0 GB+ / >78s before it ran
  the disk out). The `all` form pulls test dependencies of dependencies that no
  build here compiles. Narrowing it to `go mod download` would make the CI warm
  step cheaper, and is worth trying — but the step exists to absorb transient
  proxy `INTERNAL_ERROR` storms on the direct deps (dagnats, goakt), which the
  plain form already covers. **Left as a spike, not applied:** the warm step's
  own comment says it was added after a 268s download failure, so shrinking it
  trades a measured 10s for an unmeasured flake risk. Do it only if the warm
  step is observed over ~10s on a cache hit.

And one lever whose local optimum was already documented, so it was not redone:

- **The `prod-image` Docker layer cache.** `deploy/Dockerfile.prod` layers
  `go.mod`/`go.sum` before `npm ci` and the build, which looks like it should
  benefit from a BuildKit cache. `scripts/smoke-prod-image.sh` records the
  measurement: `COPY . .` plus the CGO build invalidate every commit, so a
  github-actions layer cache saved nothing while `setup-buildx` cost 21s of the
  job. Re-adding it would be re-losing a measured 21s.

## Wall-clock reality check

The wall is 198s and **three jobs finish within 5s of each other** (run
41194803602: `browser group 1` at 11:00:04, `prod-image` at 11:00:05, `race
shard 0` at 11:00:09). That is the single most important number on this page for
deciding what to do next: buying 20s on one of them buys **zero** seconds of
wall. A change has to drop one job well below the other two, or drop all three,
before the pipeline gets faster. This is why the doc's recommendation is a
correctness guard rather than a speed lever — and why "just try the obvious
optimizations" would have produced three diffs and no faster CI.

## Rejected with the reason, not by omission

- **Delete tests.** Owen's agent audited 20,000 cases and "found almost nothing
  worth cutting, and the time was never in the tests anyway." Same shape here:
  `internal/installer`'s cost is copy-and-vet of the whole repo per unit, not
  test count. Cutting tests would trade the only real gate for nothing.
- **A cheaper/faster CI engine off GitHub Actions.** His lever 1 implies it and a
  reply in the thread makes it explicit. It is a migration with a self-hosting
  cost, against a 3.3 min wall, and it breaks the "free" constraint the moment
  the runner is not GitHub-hosted.
- **Nightly full-suite drift.** His lever 8 pairs affected-only runs with a
  nightly full run. `ci.yml` already runs the full suite per PR (the shard split
  divides packages, it does not skip them), so there is no selection to drift.

## What this page does not change

`ci-wall-clock-2026-10.md` owns the cost table and the remaining levers (trimmed
-tree shards, the `prod-image` compile, `GOCACHEPROG`, faster runners). This page
is the cross-check against a different author's lever list, and its only two
outputs are the classifier test and a measurement to take. Nothing here
supersedes that page's numbers.

## Revisit triggers

- A `race` shard approaches its own setup cost, or a package becomes a third of a
  shard: re-run the cost table, not this page.
- The classifier test lands: if it cannot be made to fail by mutating the regex,
  the test is testing the helper and not the contract (skip logic that reports
  green while skipping is the whole bug class).
- GitHub-hosted queueing shows up in `gh run view --json jobs` as a multi-minute
  wait for a machine: lever 1 stops being a cost decision.
- The repo's `.git` grows enough that a depth-1 checkout lands in the seconds
  range, re-open item 2 with numbers.
