# Local CI

Pushing to `master` triggers GitHub Actions: `ci.yml` runs the full gate while
`deploy.yml` builds + ships to production in parallel — the deploy does **not**
wait for CI, so the pre-merge PR check is the gate that runs before code lands.
Run that **exact gate on your own machine** before pushing, so you don't wait
on remote runners and don't push a broken commit.

The `pull_request` trigger carries **no base-branch filter**: a stacked PR
(base = another feature branch) ran no check at all while the filter said
`branches: [master]`, so its diff could only be judged by reading it. Every PR
now gets the same gates regardless of base.

## gh-signoff

We use [gh-signoff](https://github.com/basecamp/gh-signoff) — a GitHub CLI
extension that stamps a green commit status after your local gate passes.

```bash
# one-time: install the extension
gh extension install basecamp/gh-signoff

# before pushing: run ci-local, then stamp the commit green
make signoff
```

`make signoff` runs `make ci-local` and then stamps the current commit green
with `gh signoff`. The dependency runs one way: **`signoff` calls `ci-local`;
`ci-local` never calls `signoff`** — that keeps the local gate clean to run on
its own and reserves the git stamp for the explicit pre-push moment.

> `make ci-local` uses `golangci-lint` as the authoritative formatter/lint gate
> (the same linter CI runs) rather than the standalone `gofumpt` binary, which
> can be a newer release than the one golangci-lint bundles and would otherwise
> produce false-positive listings.

> **Signoff is advisory, by design.** This repo deploys on **push to `master`**
> (not PR merge), so the signoff status is a *signal*, not a hard gate. We do
> not run `gh signoff install` (which would gate PR merges) — it would be
> meaningless for a push-to-deploy flow, so we leave it off.

## The feedback tiers

Cascade up as confidence grows. Each tier catches a different failure class;
lower tiers are ~10× cheaper, so promote only when (a) you are about to
push/merge, or (b) the change touches an area the next tier checks.

| Tier | Command | Cost | Catches |
|---|---|---|---|
| **T1** format + build | `gofumpt -l -d <files>` + `go build ./...` | ~10s | Format drift, compile errors |
| **T2** lint scoped | `go vet` + `make lint-safe` (host-aware: scoped + memory-capped) + `make templ` / `make datastar-lint` (when `.templ` changed) | ~15–20s | Shadow, mnd, nolintlint, revive, staticcheck, Datastar attribute mistakes |
| **T3** tests scoped | `go test -race -count=1 <changed-pkg>` | ~5–30s | Race detector on tests, business logic |
| **T3.5** fast gate | `make ci-local-fast` | **~2–30s** | T1+T2 for the whole repo's cheap checks, plus race tests for **only the changed packages** (auto-detected) |
| **T4** full local gate | `make ci-local` (refuses first via `check-host-capacity` when the box cannot carry it) | ~6min | Full pre-push check (= CI) |
| **T5** signoff local | `make signoff` (= T4 + `gh signoff -f`) | ~6min | Same as T4, plus it commits the verification to git |

### Why the full gate is slow, and the fast sibling

The full run is dominated by `go test -race`. Measured `ci-local`:
**5:50–7:11** end to end (2026-10-12, 10-core Mac, two consecutive green
runs — the range itself is load variance, see the flake paragraph below;
Playwright suites run in parallel via `scripts/ci-fanout.sh`, so the
wall-clock is the slowest suite, not the sum). Heaviest race packages by
package time (they overlap in parallel — wall is the max, not the sum):
`internal/auth` ~292s (argon2id cost is intentional work, ~250ms per
hash by calibration), `internal/installer` ~193s (trim build gates copy
and vet the whole repo per unit), `features/notes/handlers` ~188s,
`features/todo/handlers` ~173s, `features/whiteboard/handlers` ~168s,
`rules` ~110s (per-rule lint fixtures). Everything else is cheap:
templ/regens <1s, css-check ~2s, lint ~8s, boot smoke ~14s. (The old
~82s total predates the fan-out and the current suite size — do not quote
it.)

Browser suites under fan-out load can flake on wall-clock waits
(IndexedDB drains, SSE swaps with 10-20s timeouts starve while the race
suite hogs the machine — measured 2 in 4 parallel runs, 0 in 3 solo
runs, always green on retry). `scripts/ci-fanout.sh` reruns a failed
browser suite exactly once, serially, and labels a pass-on-retry as a
flake; two consecutive failures mean the product regressed (the race
suite never retries — a retried race is a hidden data race). Offline
waits log their elapsed time (`⏱ <step>: <ms>` in `smoke.mjs`) so the
next flake arrives with durations attached. The structural fix is
deterministic convergence, not tolerance: the SW posts `sync-end` when
its queue drains, the banner bridge dispatches `gogogo:synced`, and the
shared resync button refetches — offline tabs stay visible (so the
visibility refetch never fires) and a replay broadcast landing mid
re-establishment is missed with nothing after it.

`make ci-local-fast` runs the same cheap-but-decisive checks
(`templ`, `datastar-lint`, `css-check`, `check-scope`) plus **scoped** lint and
race tests, narrowed by `scripts/changed-packages.sh` to the packages your
diff actually touches. Measured: **~2s** for a CSS-only change, **~10s** for a
single-package Go change, versus ~6min full. It falls back to all packages when
a shared file moves (`go.mod`, `config/`, `internal/db/`, `internal/capabilities/`) or
when nothing changed.

Use it while iterating; the full `ci-local` (and `make signoff`) remain the
authoritative pre-push gate — `ci-local-fast` does not replace CI coverage, it
just stops a CSS tweak from paying for the todo suite.

`ci-local-fast` carries its own guards: a `--fast` capacity pre-flight
(lighter floors — 3 GB RAM, 2 cores, 1.5 load/core, 1 GB disk) refuses a
wedged box before scoped race starts, and past 8 changed packages
(`FAST_MAX_PARALLEL_PKGS`) the race run degrades to serial (`-p 1`) to
bound shared-host load. Refused means the same as T4: scope to one
package by hand, or let the PR's CI be the gate.

Heavy Go tests guard themselves too: `internal/hostgate.SkipIfConstrained`
makes repo-copying suites (notably `TestTrimmedTreeStillBuilds`, one full
`go vet ./...` per unit) skip — with measured reason — when off-CI and
constrained, instead of wedging a shared box no Makefile gate can see.

### Homolog light: staging without a server

There is no staging environment by design — a second stack would duplicate
ports, data dirs, secrets, tunnel routes and memory on the shared box while
answering a question the local loop already answers in seconds. When you need
to "see it working" (including a clickable URL for a human), run this:

```bash
make dev                          # live-reload on :8080 (own DATA_DIR, never prod's)
make ci-local-fast                # gate the change: cheap checks + scoped race tests
node scripts/smoke.mjs            # boot the real binary, exercise offline replay
command -v tailscale >/dev/null && tailscale serve --bg 8080 || echo "no Tailscale here: share http://localhost:8080 instead"
tailscale serve status            # the URL to share (skip if no Tailscale)
tailscale serve reset             # take it down when done (+ `make smoke-clean`)
```

Tailscale ([tailscale.com/download](https://tailscale.com/download)) puts
your machines on a private WireGuard mesh. It is **optional locally** (only
for the shareable URL above) and **required for server deploys**, where CI
reaches the box over the tailnet instead of a public SSH port
(`docs/deploy.md`).

Use `SIMULATE_LLM=true` for the keyless LLM path, and prefer the PR flow for
anything beyond a look-and-click: PR CI is the gate that runs before merge,
while a direct `master` push ships without waiting for it.

### Machine-safety map

Every gate probes the machine with evidence and degrades instead of wedging
it. The contract, in one place:

| Gate | Probe | Bound | Fallback | Tells you |
|---|---|---|---|---|
| pre-commit (lint) | free RAM + cores (`lint-safe.sh`) | GOMEMLIMIT/GOMAXPROCS/nice/cgroup cap | changed packages only | evidence line on every run |
| `ci-local-fast` | `--fast` pre-flight (3 GB / 2 cores / load 1.5 / 1 GB) | serial `-p 1` past 8 pkgs | single-pkg runs + PR CI | refuse message + override |
| `ci-local`, `test-web.sh` | full pre-flight (6 GB / 4 cores / 0.75 / 2 GB) | refuses first | fast + PR CI | refuse message + override |
| heavy Go tests | `hostgate` (RAM/load/disk, tunable) | skip, never force | CI never skips | `Skipf` reason + fan-out summary |
| CI jobs | none needed — isolated GitHub-hosted runners | browser serial in-job, race never retried | per-job retry | — |
| homolog-light | none needed — heaviest step is one binary build | — | localhost URL without Tailscale | — |

No global pre-commit guard by choice: refusing hooks would skip
`secrets-scan`, which must see every commit — the heavy job (lint) is capped
instead, and `govulncheck` only runs on `go.mod`/`go.sum` changes.

### Where T4 may run (host-capacity pre-flight)

- **`make ci-local` refuses first on a box that cannot carry it.**
  `scripts/check-host-capacity.sh` (the `check-host-capacity` prerequisite)
  measures free RAM, cores, 5-minute load and free disk, then fails with the
  offending dimension and the override. A small shared host (lint peak alone
  is ~2.6 GB RSS; T4 stacks that with the race suite plus three Chromiums)
  would otherwise pin at 99% CPU and stop answering, starving production and
  co-tenants — that outage is what the gate exists to prevent.
- **Refused != blocked.** Run `make ci-local-fast` locally and let the PR's
  CI be the full gate. `CI_LOCAL_FORCE=1` overrides (thresholds via
  `CI_LOCAL_MIN_RAM_MB` / `CI_LOCAL_MIN_CORES` /
  `CI_LOCAL_MAX_LOAD_PER_CORE` / `CI_LOCAL_MIN_DISK_MB`), same convention as
  `scripts/lint-safe.sh` one level down.
- **The suite script guards itself too.** `scripts/test-web.sh` runs the same
  pre-flight first (agents kept invoking it directly, bypassing the `make`
  gate): it refuses off CI runners unless `CI=true` or `ALLOW_HEAVY=1`.
- **Hygiene is automatic, and it cannot disturb a live run.** `ci-fanout.sh`
  traps `INT`/`TERM` to kill its four suites with it (no orphans when the
  gate is interrupted) and deletes its log dir on a green run (a red run
  keeps every log for diagnosis). Both gates reap mtime-stale smoke corpses
  at start (`reap-stale`, default 6h — a concurrent gate's fresh artifacts
  are never touched). Caches (go build, golangci, npm, Playwright browsers)
  are never listed anywhere, so hygiene never forces a rebuild.
  Go-test orphans (`/tmp/Test*`, `/tmp/go-build*` from aborted runs —
  `t.TempDir` only self-cleans on pass) reap at 24h (`TEST_STALE_MIN`,
  longer margin since `Test*` is a generic prefix on a shared box).
  `make smoke-clean` stays the manual full clean.

### Why the browser smoke test stays on Playwright

The browser engine was re-evaluated (2026-10) because `scripts/smoke.mjs` costs
~8.9s. Two lighter candidates were measured against the real server, and both
were rejected for a *capability* reason, not a speed one:

| | Playwright | ghostchrome | Lightpanda |
|---|---|---|---|
| `navigator.serviceWorker` | ✅ | ✅ | ❌ `undefined` |
| `caches` (Cache Storage) | ✅ | ✅ | ❌ `undefined` |
| offline emulation | `context.setOffline()` | ❌ no command | ❌ |
| warm command latency | ~1.3s (Node per call) | ~0.03s | ~0.1s |
| full smoke run | 8.9s | — | — |

Lightpanda has no `serviceWorker` and no Cache Storage — its own README
`Status` list omits both — so the offline path (Service Worker + IndexedDB
outbox + replay on reconnect) cannot run at all. ghostchrome drives real
Chrome and is ~300× faster per call, but exposes **no** offline-emulation verb,
and `scripts/smoke.mjs` calls `context.setOffline(true)` seven times; it is
also a CLI, not a library, so it cannot assert in-page JS state.

The cost is not the problem anyway: 8.9s is ~10% of `ci-local` next to
`features/todo`'s ~28s. Switching would trade the coverage that makes the test
worth running for a number nobody is waiting on. Re-evaluate only if the smoke
test grows past ~30s, or if a candidate ships offline emulation.

### Two test-harness footguns that masqueraded as slowness

Both were found while investigating why `features/todo` took 148s. Neither was
really a performance problem — each was a bug that made a test wait for
something it would never get, or for longer than it asked.

**1. A parked `Read` ignores the deadline.** An SSE pump written as

```go
for time.Now().Before(deadline) {
    n, err := stream.Body.Read(buf) // blocks until the next event
    ...
}
```

does not honour `deadline`: the condition is only re-checked *after* a Read
returns, so a silent stream waits one full heartbeat
(`config.DefaultSSEHeartbeatInterval`, 15s) regardless of the window asked for.
A caller requesting 6s got 15.5s. The fix is to read in a goroutine and
`select` on a timer, closing the body on expiry so the reader unblocks — see
`features/todo/handlers/stream_propagation_test.go` (goroutine + `select` on
`time.After`), guarded by ruleguard's `BlockingReadBehindDeadline`.

**2. Parsing the SSE transcript as JSON.** A transcript is `event:`/`data:`
lines, and each Datastar payload additionally carries a literal `signals `
prefix (`datastar.SignalsDatalineLiteral`). So both
`json.Unmarshal(transcript, …)` and `json.Unmarshal(payload, …)` always fail —
and a predicate built on them can never fire, silently burning its entire
timeout. Use `sseSignalPayloads`, which strips both layers.

Both bugs inflate wall-clock while looking like a slow suite, so the fix is a
helper with the guarantee, not a shorter timeout.

**3. `networkidle` on a page that opens an SSE stream.** `whiteboard.js` and
`notes.js` both create their `EventSource` on script eval, and Playwright's
`waitUntil: "networkidle"` needs 500ms with zero open connections — an open
SSE stream never lets that happen. `page.goto` then burns its whole 30s
timeout and flakes (3x in CI on `/whiteboard/wb-order-*`, 2026-10). Every
other harness (`smoke.mjs`, `todo-docwatch`) already used `waitUntil: "load"`,
which is why only the whiteboard suite failed. Same shape as the two above:
the fix is a helper with the guarantee — `gotoBoard()` / `gotoNote()` navigate
with `load` and then wait for the canvas / textarea explicitly — not a longer
timeout.

### Tests run in parallel across packages

The suite used to run as one `-p 1` sweep "for DagNats engine stability". That
diagnosis was wrong: the engine packages bound **fixed ports** (18091/18097/
18098/18099) and two packages grabbed the same 18099, so under `-p N` one lost
the bind with `address already in use`. It read as starvation; it was a port
collision.

Every DagNats test now binds an **ephemeral** HTTP port (`127.0.0.1:0`) and
reads the real address back from `srv.HTTPAddr()`. Nothing needs serializing:
`scripts/test-web.sh` is now a plain parallel run — measured **~1m50 vs ~4m15**
for the old sweep (verified stable across repeated runs and at `GOMAXPROCS=2`
and `=4`). Two tests must still use a fixed NON-HTTP port: NATS itself, because
the test has to name it to connect; those are distinct (4222 vs 4223 vs 4224 vs
14222) so they do not clash. `make coverage` keeps `-p 1`: a single
`coverage.out` needs every package in one invocation.

T2 must be green before T3 — lint and format errors fail the build downstream,
so running tests on a known-linted codebase saves re-runs.

**The order:** edit → `make ci-local` (while iterating) → `git commit -F /tmp/msg`
→ `make signoff` → `git push origin master`.

The remote CI becomes a parallel validator and the auto-deploy driver, not the
primary gatekeeper. If green locally, push without holding your breath.

## What the local gate and the remote CI each actually run

The claim above ("CI runs the same checks as `make ci-local`") is a contract,
not a coincidence — and it silently broke twice. The two jobs are kept in sync
by hand, so when a step is added to one, add it to the other:

| Check | `make ci-local` | GitHub CI |
|---|---|---|
| templ generate | ✓ | ✓ |
| generated `_templ.go` matches sources | ✓ `check-generated` | ✓ (also pre-commit, `**/*.templ`) |
| CSS bundle matches sources | ✓ `css-check` (all skins) | ✓ (after `npm ci`) |
| SCOPE annotations | ✓ | ✓ |
| Flat-stack deps (no banned framework, no cross-feature imports, owned sql.Open) | ✓ `check-flatdeps` | ✓ |
| Prose claims (no hand-written counts, live paths, real subcommands, docs in the MANIFEST) | ✓ `check-docs` | ✓ |
| DagNats doc pin vs go.mod | ✓ `check-dagnats-pin` | ✓ |
| SKILL.md frontmatter | ✓ | ✓ |
| install.sh bootstrap contract | ✓ | ✓ |
| datastar-lint | ✓ | ✓ |
| vendored stelow skill matches its pin | ✓ `check-stelow-drift` | ✅ `stelow-drift.yml` (weekly, `--strict`) |
| golangci-lint (33 linters) | ✓ | ✓ |
| govulncheck (dependency CVEs) | ✅ pre-push hook | ✓ |
| deadcode (advisory, host-gated + version-pinned) | ✅ pre-push hook | ✓ |
| `go test -race` (parallel, all pkgs) | ✓ | ✓ |
| build + **binary** boot smoke | ✓ | ✓ |
| browser smoke (Playwright) | ✓ | ✓ |
| whiteboard client (Playwright, canvas pixels) | ✓ | ✓ |
| SSE verified-open (slow-init opener proof) | ✓ | ✓ |

CI runs the rows above as four parallel jobs (`checks` | `race` | `browser` |
`prod-image`) — wall-clock is the slowest job, not the sum. A `ci`
aggregator job (green only when all four are) preserves the single status
context branch protection requires.

Those four skip on a **docs-only changeset** — a `changes` classifier job reads
the PR (`base...HEAD`) or push (`before...after`) diff and marks it when every
path is `docs/`, `site/`, `*.md`, `LICENSE`, `.stelow/` or `.gitignore`; an
unreadable diff (force-push, shallow history, dispatch) fails **open** to a full
run. A trigger-level `paths-ignore` would be cheaper but skips the workflow
entirely, leaving the required `ci` status pending forever on a docs PR — the
classifier always runs, so `ci` always reports. Each job provisions its own
toolchain and deps (`bin/check-ci-deps.sh`, pre-commit + CI, fails a job that
uses `node`/`go` without installing them first).

`make ci-local` runs the same set locally, cheap steps first, then the
race+browser fan-out (`scripts/ci-fanout.sh`).

The docs site is not part of `ci-local`. `pages.yml` runs `site-check`, builds
`site/` (including the generated search index under `site/search/`) and runs
`scripts/site-layout.test.mjs` before publishing. `docsite-drift.yml` checks the
vendored engine against `site/UPSTREAM_SHA` on PRs that touch it and weekly;
`docsite-bump.yml` opens the bump PR.

**The generated-artifact checks are the ones that regressed.** The generated
`_templ.go` files and the three committed CSS bundles (`app.min.css`,
`basecoat.min.css`, `shadcn.min.css`) are *committed and embedded in the
binary*. Both CIs used to only **generate**
them and then run every subsequent step against the freshly generated output —
so a `.templ` (or `input.css`) edit whose regeneration was never committed
passed lint, passed tests, passed both smoke tests, and shipped a binary
rendering stale markup. Both now regenerate and **diff**, which is exactly what
`css-check` does.

**The Basecoat bundle slipped through that same hole a second time.** CI and
`bin/check-css.sh` built only `app.min.css` while their diff named both files,
so `basecoat.min.css` was compared against itself — the diff was empty by
construction and could never fail, however stale the committed bundle was (it
was, by 7 lines, until v0.51.0). `make css-check` builds `css-all` and did
catch it, which is why CI and the hook disagreed. CI, the hook and
`make css-check` now all run `css-all`: one command, no drift.

This is deterministic, and that matters: the reason `actions/cache` is disabled
in this repo (`skip-cache: true`, `cache: false`) is a *real, documented*
history of flaky tar restores (`/usr/bin/tar` exit 2) aborting jobs — see
commit `d50d0fb`, where caching was turned off deliberately rather than
overlooked. Do not re-enable it to save seconds without reading that first;
the CSS/templ diffs above add no such risk because they compare text, not
extract archives.

Cold caches have a second cost: every Go job downloads the full module graph,
and the proxy answers those storms with transient HTTP/2 `INTERNAL_ERROR`s
(twice in one day: a 156s `go list` death, a `go test` naming the dagnats and
goakt zips). Two mitigations, in `ci.yml` and the image builder: `GOPROXY`
uses the pipe form (`proxy|direct`, so a proxy error falls back to git —
comma only falls back on 404/410), and every Go download leg warms with
bounded retries (`bin/go-mod-download.sh` in CI jobs, the same loop inline
in `Dockerfile.prod`, which has no bash) — still failing fast on a real
outage. `vendor/` stays out: hermetic, but the tree, the template clones
and the scanners all pay for it on every change — re-evaluate only if
proxy flakes survive the retry.

## When CI goes red

1. Read the failing log step (test, lint, css-check, build).
2. Reproduce locally with `make ci-local` — usually the same failure.
3. Fix and commit.
4. `make signoff` again — green means it would pass on a re-run.

## Post-push: verify what shipped

Deploy fires on push to `master` without waiting for CI, so the pre-push
gate is half the story. After pushing, run:

```bash
make verify-ci
```

It checks default-branch CI plus the live `/health` endpoint with a
bounded wait (never `gh run watch`), prints one JSON line, exits 0/1.
Pending (CI outruns the cap) means re-run later, not failure. PR
branches skip this — they verify through the PR's own CI gate instead.
Engine: the `cali-ops-verify-ci` skill script, pinned here to
`calionauta/gogogo` + the live URL.

## What the local loop has caught

CI runs the same checks as `make ci-local`. What signoff does is run them
**locally first**, so a regression shows up in your terminal in 1–3 min instead
of after a CI queue. The biggest emitters:

- Race detector on `TestXxx` (e.g. `sync.Once` used instead of TOCTOU init).
- Dockerfile `ARG` inline placement (`ARG X=foo` inside a `RUN … && …` chain
  fails Buildkit parse).
- Stale `-tags jetstream dagnats` after the unified-build era — silent in Go
  compile, silent in lint, visible only on `docker buildx`.
- CSS bundle silently stale (a `.templ` or `.go` file changed but
  `app.min.css` was not regenerated).
- Format drift accumulating through several small commits.

The difference between CI and signoff is **who waits** for the run, nothing else.

## Git hooks

`make setup` activates the lefthook hooks (it sets `core.hooksPath=.githooks`
and regenerates the wrappers; the wrappers are committed and are a graceful
no-op when lefthook is not installed).

| Hook | Jobs | When |
|------|------|------|
| `pre-commit` (parallel) | file-sizes · fmt-gofumpt · mod-tidy · install-sh · scope-lint · flatdeps-lint · docs-claims · secrets-env · secrets-scan · rbac-policy · dagnats-pin · skill-frontmatter · datastar-lint · api-routes · registry-conformance · generated-templ · css-check · golangci-lint (host-aware, `scripts/lint-safe.sh`) · agents-md-staleness · docs-staleness (advisory) · stelow-drift (advisory) · govulncheck/ci-deps (glob-scoped) | every commit; glob-filtered jobs skip when nothing relevant is staged |
| `pre-push` | govulncheck · deadcode | every push |

The `deadcode` job is ADVISORY and **host-gated**: it compiles every package
(measured 30s wall / 175% CPU / **3.28 GB peak RSS** — more than a full-repo
lint), so on a box without that headroom it prints `SKIPPED` with the failed
dimension and lets the push through. `DEADCODE_FORCE=1` overrides, and
`DEADCODE_MIN_RAM_MB` / `DEADCODE_MAX_LOAD_PER_CORE` move the floors. Same
evidence-over-labels contract as `scripts/lint-safe.sh` and
`scripts/check-host-capacity.sh`, and for the same reason: committing 3.3 GB
on a shared host makes the scan swap, and before this gate existed a real
push sat in that hook for 23 minutes at load 18 on 4 cores. The contract is
pinned by `bin/check-deadcode_test.go` (skip verdicts, named dimension,
override discoverability, and the invariant that the hook never fails a
push).
| `post-merge` | regen-assets (templ + css-all when templ/go/css changed) | after pulls/merges |

External tools are **version-pinned in `go.mod`** (the `tool` directive, already
used for `templ`) and run through `go tool`, never `go install @latest`. That is
what keeps a scan from disagreeing with the toolchain about which module
version it is type-checking — the mismatch that made `deadcode` report
type-check errors as "dead code found". Add another tool the same way:
`go get -tool <pkg>@<version>`, then call it as `go tool <name>`.

The `deadcode` job is ADVISORY and **host-gated**: it compiles every package
(measured 30s wall / 175% CPU / **3.28 GB peak RSS** — more than a full-repo
lint), so on a box without that headroom it prints `SKIPPED` with the failed
dimension and lets the push through. `DEADCODE_FORCE=1` overrides, and
`DEADCODE_MIN_RAM_MB` / `DEADCODE_MAX_LOAD_PER_CORE` move the floors. Same
evidence-over-labels contract as `scripts/lint-safe.sh` and
`scripts/check-host-capacity.sh`, and for the same reason: committing 3.3 GB
on a shared host makes the scan swap, and before this gate existed a real
push sat in that hook for 23 minutes at load 18 on 4 cores. The contract is
pinned by `bin/check-deadcode_test.go` (skip verdicts, named dimension,
override discoverability, and the invariant that the hook never fails a
push).

> If the pre-commit hook reports every job green but still exits non-zero, read
> the **middle** of the output, not the summary — the jobs run in parallel and
> the summary block prints last. A `go get` that leaves stale `go.sum` lines is
> the usual culprit; `go mod tidy && git add go.mod go.sum` fixes it.

## Related

- [Code quality](code-quality.md) — the 33 linters and how to run them scoped.
- [Troubleshooting](troubleshooting.md) — when the gate is green but behavior isn't.
- [Deploy](deploy.md) — what happens after the push.