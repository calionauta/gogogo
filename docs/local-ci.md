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

> **Signoff is advisory, by design.** Production deploys on a **release**
> (`.github/workflows/deploy.yml` is dispatched by release-please with the tag
> it just cut, and a push to `master` does not deploy), so the signoff status is
> a *signal*, not a hard gate. We do not run `gh signoff install` (which would
> gate PR merges) — a merge gate says nothing about a release that is cut from
> an already-green `master`, so we leave it off.
>
> (This paragraph used to say the repo "deploys on push to `master`" and called
> this a "push-to-deploy flow". That was wrong: `deploy.yml`'s header records
> that the old trigger was a push with a path allow-list, and that the unit of
> deployment became the *release* precisely so the badge could not name a tag
> the box was not running. The conclusion — install no merge gate — survived the
> correction; the reason given for it did not.)

## The feedback tiers

Cascade up as confidence grows. Each tier catches a different failure class;
lower tiers are ~10× cheaper, so promote only when (a) you are about to
push/merge, or (b) the change touches an area the next tier checks.

Seven stops in cost order, one integer each. Two of them used to carry a
fractional label, which read as "sub-steps of the tier before" when neither is
one: the fast gate re-runs T1+T2 repo-wide, and the operator's click is not a
test at all. They were renumbered rather than kept, because a label that has to
be explained is doing no work.

| Tier | Command | Cost | Catches |
|---|---|---|---|
| **T1** format + build | `gofumpt -l -d <files>` + `go build ./...` | ~10s | Format drift, compile errors |
| **T2** lint scoped | `go vet` + `make lint-safe` (host-aware: scoped + memory-capped) + `make templ` / `make datastar-lint` (when `.templ` changed) | ~15–20s | Shadow, mnd, nolintlint, revive, staticcheck, Datastar attribute mistakes |
| **T3** tests scoped | `go test -race -count=1 <changed-pkg>` | ~5–30s | Race detector on tests, business logic |
| **T4** fast gate | `make ci-local-fast` | **~2–30s** | T1+T2 for the whole repo's cheap checks, plus race tests for **only the changed packages** (auto-detected) |
| **T5** the operator's click | `make verify-local` | ~60s of human time | Builds the real binary on a fresh database, prints a URL, one account and the routes the diff touched — the judgment no gate replaces (see [When the operator must click](#when-the-operator-must-click-and-how-to-ask)) |
| **T6** full local gate | `make ci-local` (refuses first via `check-host-capacity` when the box cannot carry it) | ~6min | Full pre-push check (= CI) |
| **T7** signoff local | `make signoff` (= T6 + `gh signoff -f`) | ~6min | Same as T6, plus it commits the verification to git |

### After the merge: did the docs site actually publish

The ladder above stops at the push. The site publishes from `master` on a path
filter (`docs/**`, `site/**`), so a merge that touched either owes a publish and
a merge that touched neither does not.

    make check-site-deployed

It reads the Pages deployment record rather than the rendered page, because the
rendered page cannot tell you *which commit* you are looking at, and a run
cancelled by the concurrency group reads as "not deployed" even when the later
run published both changes. Exit 0 means the site is on master's tip or on a
tip that owes no publish; exit 1 names the unpublished commits. It needs network
and `gh` auth, so it is deliberately **not** part of `ci-local`.

### Do the new tests pin anything (test-value + diff-cover)

The race suite proves the tests pass; two review aids prove they pin:

- `make check-test-value` — AST-only (~1s, no coverage run): fails on a
  **vacuous** Test (no failure call — green unless it panics) and warns on an
  **err-only** Test (asserts `err == nil` and nothing else). Same binary runs
  in pre-commit (`-quiet`, blocking on vacuous only) and CI. Normative text:
  `skills/gogogo-coding-standards/references/go-testing.md`.
- `make cover-diff` — **advisory, on-demand**: added production lines no test
  executes, plus test-value on the changed packages. Not in any hook or CI —
  a coverage run per commit would tax the shared box for a signal only a
  human review consumes.

### Why the full gate is slow, and the fast sibling

The full run is dominated by `go test -race`. Measured `ci-local`:
**5:50–7:11** end to end (2026-10-12, 10-core Mac, two consecutive green
runs — the range itself is load variance, see the flake paragraph below;
Playwright suites run in parallel via `scripts/ci-fanout.sh`, so the
wall-clock is the slowest suite, not the sum). Heaviest race packages by
package time (they overlap in parallel — wall is the max, not the sum):
`internal/auth` ~292s (argon2id cost is intentional work, ~250ms per
hash by calibration), `internal/installer` ~193s (removal gates copy
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

### Reading a flake

`scripts/run-browser-suite.sh` reports a failure by its **marker**, never by
position in the log. Each suite writes one through `console.error` (`✗` in the
node suites, `❌` in `smoke`/`chat-scroll`), so the wrapper extracts those lines
from anywhere in the file. It used `tail -n 25` until 2026-11-16, and CI run
43870137994 shipped the trap: `notes-caret` closes with 23 consecutive green
`✓` lines, they filled the whole window, and the step announced “FAILED twice”
while printing **zero** failed assertions (33 of that suite’s 81 assertions run
before the window even starts). A log with no marker is now reported as such —
that is a suite that died before reporting (a throw, a crash, a supervisor
kill), which is a finding, not a green tail.

**Both attempts are traced, and the first one is the point.** The wrapper pins
`SMOKE_TRACE` for the first attempt (`<name>.trace.zip`) and the retry
(`<name>.retry.trace.zip`); `scripts/lib/trace.mjs` writes one file per traced
context (`<base>-<label>.zip`) because Playwright writes one file per
`tracing.stop` and these suites are multi-context by design. The **failed**
attempt is the one whose timeline holds evidence — a trace of the run that
passed records nothing going wrong — so the retry-only trace this started as
(until 2026-11-16) collected the wrong run.

Both matrix legs upload their `CI_LOG_DIR` as the `browser-logs-group-N`
artifact on `if: always()`. A flake *passes*, so a failure-only upload would
discard exactly the run that needs explaining. The whole contract is asserted
by `scripts/check-run-browser-suite.test.sh` (in `make check-ci-scripts`),
which also **fails if a suite CI runs ignores `SMOKE_TRACE`** — the state three
of the five were in until 2026-11-16. Run it after touching the wrapper, any
browser suite, or the `browser:` job.

`make ci-local-fast` runs the same cheap-but-decisive checks
(`templ`, `datastar-lint`, `css-check`) plus **scoped** lint and
race tests, narrowed by `scripts/changed-packages.sh` to the packages your
diff actually touches. Measured: **~2s** for a CSS-only change, **~10s** for a
single-package Go change, versus ~6min full. It falls back to all packages when
a shared file moves (`go.mod`, `config/`, `internal/db/`, `internal/capabilities/`) or
when nothing changed.

That narrowing is deliberately narrower than CI's: here it means "lint and test
what I touched", while CI runs the touched packages **plus every package that
depends on them** (`--with-dependents`) — a local run cannot see the caller 40
packages away, and a PR can. CI reaches the same machinery through two env vars
if you need to reproduce a shard locally: `TEST_PKGS_FILE` (the scope
`scripts/test-web.sh` runs) and `SHARD_UNIVERSE_FILE` (the list
`scripts/ci-shard-packages.sh` splits).

### When the fast gate stops being fast

A shared-file change (`go.mod`, `go.sum`, `Makefile`, `config/`,
`internal/capabilities/`, `internal/db/`) makes `changed-packages.sh` fall back
to **all** packages. Above `FAST_MAX_PARALLEL_PKGS` (default 8) the target then
runs `go test -race -p 1` on purpose, to bound load on a co-tenanted host — that
is the same suite as `ci-local`, serialized, and it can exceed ten minutes.
Two practical fixes: run the gate in the background and collect it when it
finishes, or set `FAST_MAX_PARALLEL_PKGS` to a value that lets the race suite
use the cores this host actually has (only on a dedicated box). The 10-minute
cutoff some agents hit is the tool's command timeout, not the Makefile's.

Use it while iterating; the full `ci-local` (and `make signoff`) remain the
authoritative pre-push gate — `ci-local-fast` does not replace CI coverage, it
just stops a CSS tweak from paying for the todo suite.

### A feature's recipe id has one spelling: `TestRecipeIDConvention`

Every in-tree feature names itself once, beside the declaration that reports it:

```go
// features/<name>/handlers/
const RecipeID = "<id>"
```

That id is three things at once — the namespace the feature's DDL is applied
under (`dbschema.MigrationSet.ID`), the name its declaration answers to over
MCP, and the key its `.recipes/<id>.yml` receipt is filed under — so a fourth
spelling does not fail to compile, it silently files one feature's schema under
another name. `TestRecipeIDConvention` (in `internal/capabilities`, so it runs
in the ordinary `go test ./...` and in both `ci-local` targets) pins it in both
directions and needs no new Make target:

| Rule | Why |
|---|---|
| one `const RecipeID` per feature, in `features/<name>/handlers/` | 12 of 12 features, one spelling |
| the value is a string **literal** | the gate reads source; it does not evaluate |
| it matches `.recipes/<name>.yml`'s `id:` | constant, declaration and receipt cannot drift |
| the constant lives nowhere else in the tree | the half that catches a "helpful" move |
| a duplicate id across features fails | the ledger keys on it |

It exists because that exception shipped. `RecipeID` was briefly moved out of
`features/todo/handlers` into `features/todo` — sound about the symptom (the
native shell must not link cgo) and wrong about the cure, because it made `todo`
the one feature in twelve whose constant lived somewhere else. A per-feature
exception is the class of change the boundary gates exist to refuse. The shell
now spells the id as a local literal and pins it with
`TestTodoRecipeIDMatchesTheUnit`; test files are not compiled into the released
binary, so the assertion costs the cgo boundary nothing.

The general shape, worth reusing: **when a non-handler caller needs one value
from a package it must not link, do not move the value — restate it locally and
pin it with a test.** The test carries the coupling; the binary does not.

A scaffold template (`internal/installer/new_scaffold.go`) contains the same
`const RecipeID` because it EMITS the convention; the gate recognises template
payloads by the `"{{module}}/` placeholder and exempts them.

### The release binaries are built here too: `make release-build-guard`

`bin/check-release-build.sh` answers one question: does every binary the
release SHIPS build the way the release builds it. It reads the `main:` entries
**and each build's own `CGO_ENABLED`** out of `.goreleaser.yaml` — so a fourth
build added there cannot be forgotten here, and a build that legitimately turns
cgo on is not failed by a guard with a hardcoded `0` — then runs that
combination across linux/darwin/windows × amd64/arm64. Nothing is written
(`-o /dev/null`), and it runs after "Warm module cache" in CI, so it costs
seconds. It is part of both `ci-local` and `ci-local-fast`.

It exists because nothing did that before. `.goreleaser.yaml` cross-compiles all
three binaries with `CGO_ENABLED=0` from one Linux runner; `cmd/gui` imported
`features/todo/handlers` for a single constant (`RecipeID`) and thereby pulled in
`internal/recipe/register` → `internal/collab` → `loro-go`, which needs cgo. The
`CGO_ENABLED=0` build died on `undefined: DiffEvent`, and **every release from
v0.63.0 to v0.71.2 failed with zero assets** — the first line of the landing
page, `curl … | sh`, was a 404 for nine releases. A host-native `go build ./...`
cannot see it (the host has cgo on, and `cmd/web` is built with cgo in the
Dockerfile), and the only job that built the shipped combination was
`release.yml`, which runs after the tag exists and which nobody read.

The general rule this encodes: **a build that only works with cgo is a build
that does not ship.** If a release binary needs a cgo dependency, either remove
it from that binary's import graph (the `cmd/gui` fix) or move that binary to a
per-OS runner with cgo on — which costs the one-runner matrix, so it is the
heavier answer, not the default one.

### The other half: `bin/check-release-assets.sh`

Building correctly is necessary, not sufficient — the assets still have to
reach the release. `release-please` publishes the GitHub release when the
release PR merges, and `release.yml` attaches the binaries afterwards, so a
release whose build fails is already public when it fails. That is the shape of
the nine-release outage: `release.yml`'s own "Verify the release carries the
installer assets" step exists (it was added after v0.43.0–v0.51.0 shipped
empty), but a job that dies BEFORE that step still produces a public, empty
release, and a red run nobody reads is indistinguishable from no check at all.

`bin/check-release-assets.sh` asks the question a user asks: does the tarball
`install.sh` builds exist in the release, and does the checksum file it verifies
against exist. It reads the tarball pattern out of `install.sh` itself, so a
rename in one place cannot pass the guard while breaking the other. It has two
callers, and they are deliberately not the PR path:

| Where | When | Catches |
|---|---|---|
| last step of `release.yml` | per release | a release whose assets landed wrong |
| `.github/workflows/release-assets.yml` | daily, `:17` | a release goreleaser never attached to, or a job that died early |

Neither belongs on a pull request: a PR cannot know what the last PUBLISHED
release contains, and spending a network round trip on every push to learn a
fact that only changes on a tag is the wrong trade. The offline form
(`--offline`) exits 0 so a dev box without `gh` is never a false red.

### A recipe's tests have a name: `make test-recipe R=<id>`

The changed-package scope answers "what did this diff touch". `test-recipe`
answers a different question: "what does this slice claim, and does it still
hold". The package list is **derived** from the receipt (`.recipes/<id>.yml`
`Files` + `Register`), because the receipt already hashes every installed file —
tests included — and a declared list would be a second source of truth:

```bash
make test-recipe R=todo          # go test -race on the packages todo owns
go run ./cmd/gogogo recipe test todo --race=false --run TestToggle   # TDD loop
```

A recipe with no `_test.go` fails the run. `recipe check` blocks it as well, and
that pair is deliberate: the checker states the rule, the runner enforces it at
the moment you asked for the tests. Tests travel with the recipe (they are
ordinary `_test.go` files in the copied tree, run by `go test ./...` like any
other package), so an uninstalled recipe has no tests to skip — the grouping is
structural, not a filter someone has to maintain.

`ci-local-fast` carries its own guards: a `--fast` capacity pre-flight
(lighter floors — 3 GB RAM, 2 cores, 1.5 load/core, 1 GB disk) refuses a
wedged box before scoped race starts, and past 8 changed packages
(`FAST_MAX_PARALLEL_PKGS`) the race run degrades to serial (`-p 1`) to
bound shared-host load. Refused means the same as T6: scope to one
package by hand, or let the PR's CI be the gate.

Heavy Go tests guard themselves too: `internal/hostgate.SkipIfConstrained`
makes repo-copying suites (notably the removed-tree build proof, one full
`go vet ./...` per unit) skip — with measured reason — when off-CI and
constrained, instead of wedging a shared box no Makefile gate can see.

### When the operator must click (and how to ask)

Every gate proves what a machine can assert. None of them can tell you the flow
feels wrong, the label is ambiguous, or the feature is not what was asked. That
judgment is the operator's alone — so it belongs where a rejection is cheapest,
not wherever it happens to be noticed.

| Moment | A rejection costs | Verdict |
|---|---|---|
| Before the commit | Nothing has validated the diff yet (T2+ has not run) — you hand a human a broken build | ❌ never |
| Before `ci-local-fast` | Same; the 2–30s filter has not run | ❌ |
| **After `ci-local-fast`, before `ci-local`/`signoff`/push** | one edit + 10s. Branch is local, `make dev` is hot | ✅ **primary** |
| When the PR is open / CI is green | already paid; a fix is a new push and another PR-CI round trip | ⚠️ only if the state changed |
| **Before merge** | the PR goes back to draft; nothing shipped | ✅ **secondary** |
| After merge | it is in production: `deploy.yml` runs in parallel with CI on a `master` push | ❌ too late |

The rule that decides whether to ask at all:

- **Ask** when acceptance is not expressible as an assertion, when the action is
  irreversible for data/auth, when it needs two clients or a real browser, or
  when the visual IS the requirement.
- **Do not ask** when the diff is machine-decidable and a red test already pins
  it. Asking there trains the operator to click without looking, and trains the
  agent to push verification onto a human.

Two rules make the ask worth its cost:

1. **One click session per mergeable state**, not per TDD iteration.
2. **A 60-second script, never "test it"**: `make verify-local` prints the URL,
   the seeded account and the routes your diff touched (derived from the
   receipts, not guessed). Say what to do and what you expect; a vague ask costs
   ten minutes of human attention and answers "hmm, seems odd".

```bash
make verify-local            # build → boot on :8098 → URL + account + routes changed → wait
make verify-local-share      # same, plus a tailnet URL for a second machine
```

It builds the real binary into a temp dir, boots it on a **fresh** database, seeds
one account, prints the checklist, and removes everything when you press Enter
(agents: no TTY, so it stops after `--ttl`, default 30 min). Your data is never
touched — never point it at a live instance.

PASS → `make ci-local` (T6) / `make signoff` / push. FAIL → fix now: an edit and
ten seconds, instead of six minutes of gate plus a CI round trip.

### The look is enforced, not just advised

The table above is a judgment rule, so it is also a gate — otherwise an agent
can skip the ask and nothing notices:

- `make verify-local` prompts **PASS / FAIL / later** when it has a terminal,
  and records the answer as `.gogogo/verify-local.json` (gitignored).
- `make operator-look-pass` / `make operator-look-fail` record the verdict when
  the operator answered in chat instead (the agent runs them **only** after the
  human replied).
- `bin/check-operator-look.sh` runs in `make signoff` and the **pre-push hook**.
  It refuses when the diff touches a `.templ` page, `src/css/`, or
  `web/resources/static/chrome.css` and no PASS stamp exists — a Go-only,
  test-only or docs-only diff passes with no stamp, because a test decides it.
- The stamp names the commit **and** a fingerprint of the worktree (diff +
  untracked files, `.gogogo/` excluded). An edit after the look invalidates it,
  committed or not, so the operator's PASS can never bless different code.
- A FAIL stamp also refuses, with the recorded reason, until a new look passes.

The decision lives in one script (`bin/check-operator-look.sh`); the vocabulary
of "judgment surface" is that script's glob, deliberately narrow. A gate that
asked for every CSS tweak would train people to click without looking, which is
the failure it exists to prevent. Contract test:
`bash bin/check-operator-look_test.sh` (wired into `make check-ci-scripts`).

### Homolog light: staging without a server

There is no staging environment by design — a second stack would duplicate
ports, data dirs, secrets, tunnel routes and memory on the shared box while
answering a question the local loop already answers in seconds. When you need
to "see it working", the loop is:

```bash
make verify-local                 # the operator's 60s click (above)
make dev                          # live-reload on :8080 (own DATA_DIR, never prod's)
make ci-local-fast                # gate the change: cheap checks + scoped race tests
node scripts/smoke.mjs            # boot the real binary, exercise offline replay
```

`make verify-local-share` handles the Tailscale URL and `tailscale serve reset`
teardown itself. Tailscale ([tailscale.com/download](https://tailscale.com/download))
puts your machines on a private WireGuard mesh; it is **optional locally** (only
for the shareable URL) and **required for server deploys**, where CI reaches the
box over the tailnet instead of a public SSH port (`docs/deploy.md`).

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

### Where T6 may run (host-capacity pre-flight)

- **`make ci-local` refuses first on a box that cannot carry it.**
  `scripts/check-host-capacity.sh` (the `check-host-capacity` prerequisite)
  measures free RAM, cores, 5-minute load and free disk, then fails with the
  offending dimension and the override. A small shared host (lint peak alone
  is ~2.6 GB RSS; T6 stacks that with the race suite plus three Chromiums)
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

### How the browser is installed (and why apt is optional)

Every job that drives a browser installs it with `bin/install-browser.sh`, not
with `npx playwright install --with-deps chromium`. The order in that script is
the contract:

1. **the browser binary** (Playwright CDN or the restored cache) — the part
   the gates need, and it never touches apt;
2. **the proof** — the browser must *launch*. Every install step above can
   "succeed" while the binary cannot start (a missing library, a sandbox the
   runner forbids), and the suites downstream would fail with a message that
   names neither the cause nor the fix;
3. **the system dependencies** (apt) — reached for *only when the probe in step
   2 fails*, best effort, time-boxed (`PLAYWRIGHT_INSTALL_TIMEOUT`, default
   300s), and a loud warning when it fails. Then the probe runs once more.

Step 3 used to run unconditionally, before the probe. It cost **113s** in run
44551759905 against **11s** in run 44556795112 on the same commit — both with
the browser cache a HIT, the difference being apt alone (`Fetched 4283 kB in
9s` then a second 21.9 MB fetch, against `Fetched 21.9 MB in 1s` on warm
mirrors). The runner image already carries the libraries, so on a healthy
runner the probe passes and apt never runs.

The reason is a real incident, not a theory. On 2026-11-11 the GitHub runners
stopped resolving the Ubuntu mirrors (`archive.ubuntu.com`,
`security.ubuntu.com`, `azure.archive.ubuntu.com` — "Temporary failure
resolving") while every other host resolved fine (`packages.microsoft.com`,
`proxy.golang.org`, the npm registry, github.com). Three CI runs of PR #399
died in "Failed to install browsers" with nothing but that DNS error, while
`checks`, the three `race` shards and `prod-image` stayed green: a red build
for a reason that had nothing to do with the change under test, reproducible
on every re-run. The dependency install is a convenience — the runner image
already ships Google Chrome and therefore the shared libraries Playwright's
Chromium links against — so a broken mirror must not be able to take the
browser gate, the site gate or the Pages publish down with it.

`bin/install-browser_test.sh` pins that order with stubs for `npx` and
`node`: a browser that launches must NOT pay for apt, a launch that fails MUST
reach for it, a broken apt must NOT fail the step (and must be loud), a broken
browser download MUST fail it, a browser that cannot launch MUST fail it, and
`PLAYWRIGHT_SKIP_DEPS=1` must not call apt anyway. It runs in `make
check-ci-scripts`, so the contract is checked locally and in CI.

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
`Status` list omits both — so the offline path (the service-worker page
cache plus the page outbox in IndexedDB, replayed on reconnect) cannot run at
all. ghostchrome drives real
Chrome and is ~300× faster per call, but exposes **no** offline-emulation verb,
and `scripts/smoke.mjs` calls `context.setOffline(true)` seven times; it is
also a CLI, not a library, so it cannot assert in-page JS state.

The cost is not the problem anyway: 8.9s is ~10% of `ci-local` next to
`features/todo`'s ~28s. Switching would trade the coverage that makes the test
worth running for a number nobody is waiting on. Re-evaluate only if the smoke
test grows past ~30s, or if a candidate ships offline emulation.

**Measured 2026-10: ~30s, and the engine verdict stands.** The cost is not
Playwright overhead — it is deliberate waiting:

- The offline-queue sweep runs on `/todo` (search + queue + delete replay). It
used to fan out per page variant, which is where "the other pass covers it" was learned
to be false: identical markup with a missing class renders broken and logs
nothing. One vocabulary means the sweep is one pass.
- `verifySuggestSurvivesSlowAck` holds the POST response for 2s while the
worker's SSE result lands first — the ordering that produced the stuck
"Asking AI..." spinner — then settles 500ms to let the late patch apply. That
~2.5s is the point of the test.

So the ~30s threshold is met by *coverage*, not by engine cost: the waits would
be identical under any driver. Re-evaluate the engine only for capability.

### What the smoke test asserts at the browser level

Things no Go test can see, because the render is correct and only the browser's
evaluation is wrong:

| Assertion | Regression it pins |
|---|---|
| the offline-queue sweep covers `/todo` (search + queue + delete replay) | an offline-reset listener typo (`gogogo__queued`) — invisible to unit tests, fatal in the browser |
| `/todo`: only the create submit is born disabled | `data-attr:disabled` setting the attribute for `""`/undefined — the dead "Run durable workflow" and "Delete" buttons |
| Suggest under a 2s-delayed ack: no spinner, suggestions still on screen, ack body carries no `datastar-patch-signals` | the ack landing after the worker's terminal patch (stuck "Asking AI...", results erased) |
| server-seeded signals survive the theme bootstrap | the bootstrap replacing `data-signals` (a wiped seed is a dead control) |

### A suite that hangs is a failure, not a slow test

Every browser suite runs under a wall-clock budget: `scripts/run-browser-suite.sh`
passes `SMOKE_SUITE_TIMEOUT` (default 600s) to `scripts/lib/suite-supervisor.mjs`,
which runs the suite in its own process group and kills the whole group —
node, Chromium and the server the suite spawned — when the budget is gone. The
supervisor exits 124, the wrapper prints **TIMED OUT** instead of FAILED, and
the CI job carries `timeout-minutes: 30` as the outer fence.

Why it exists: on 2026-11-11 the whiteboard suite hung in CI. Its step ran
**44 minutes** (the job was killed with the step still in progress) while
every other run of the same suite took **20 seconds**. A suite can hang
instead of failing, because Playwright's default timeouts do not cover the
places where a hang actually happens:

| Unbounded wait | Where | Now |
|---|---|---|
| an in-page `fetch` the server never answers | `page.evaluate` has **no** default timeout | `AbortSignal.timeout(10s)` on every in-page fetch |
| `navigator.serviceWorker.ready` (resolves only on activation) | the smoke suite's SW wait | raced against a 15s deadline |
| `browser.close()` on a wedged renderer | every suite's cleanup | raced against a 10s deadline |
| the suite as a whole | the wrapper | the supervisor's budget |

The last one is the load-bearing one: without it a single hang is an infinite
step, and the runner is held for hours with a log that looks complete.

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

### A test may not import a removable package from outside it

Deleting a unit deletes its directories, so a test that imports one must live
where the deletion takes it too. A shared test that imported an optional unit's
package made the removed-tree build gate stop **compiling** once that unit was
removed.

Rule of thumb: a cross-tree test belongs to the tree it asserts, or it reaches
the optional thing through a registry and skips with a log line when it has been
removed — a silent skip is coverage loss wearing the same clothes as a pass.
Never import an optional unit's package from a package that survives the removal.

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

## Keeping the docs true: what a gate can and cannot catch

A doc drifts because nobody notices it drifted. The repo answers that with
three layers, each with a different reach — and the boundary between them is
deliberate:

| Layer | What it catches | Where |
|---|---|---|
| **Mechanical (blocking)** | A count, a size, a path, a command or a tool table the code no longer supports | `make check-docs`: hand-written counts and sizes, dead paths, invented subcommands, undocumented CLI verbs, the generated MCP tool tables |
| **Instructed (review)** | A page that should have been updated and no gate can name | the PR body names the page; the pre-commit staleness hook points at the likely one |
| **Judgment (human)** | Whether the page is true, clear and still says what the code says | review |

The mechanical layer is the only one that blocks, because it is the only one
that cannot be argued with: it compares the prose against the source. It
cannot judge meaning — a page can be mechanically clean and still lie, and
that is what the other two layers are for. Do not make the mechanical layer
bigger than it can be: a gate that reads prose has to guess, and a gate that
guesses wrong trains people to bypass it.

The one class worth killing rather than catching is **copied prose**: the MCP
tool table used to live in three places (the runtime description, the docs
page and the module README), and it drifted. Those tables are now GENERATED
from the catalog the server serves, so the copy cannot exist.

## What the local gate and the remote CI each actually run

The claim above ("CI runs the same checks as `make ci-local`") is a contract,
not a coincidence — and it silently broke twice. The two jobs are kept in sync
by hand, so when a step is added to one, add it to the other:
| Check | `make ci-local` | GitHub CI |
|---|---|---|
| templ generate | ✓ | ✓ |
| generated `_templ.go` matches sources | ✓ `check-generated` | ✓ (also pre-commit, `**/*.templ`) |
| CSS bundle matches sources | ✓ `css-check` | ✓ (after `npm ci`) |
| Registry coverage (every package owned by a capability) | ✓ `registry-conformance` | ✓ |
| A trim declares markers that still exist (drop/strip declarations match the real tree) | ✓ `TestDeclaredStripMarkersExist` / `TestDeclaredDropLinesExist` | ✓ |
| Flat-stack deps (no banned framework, no cross-feature imports, owned sql.Open) | ✓ `check-flatdeps` | ✓ |
| Product ⟂ evolution tools (`cmd/web` must build after the tool is deleted) | ✓ `check-product-boundary` | ✓ |
| One spelling per state (no named string type compared with a literal) | ✓ `check-typed-states` | ✓ |
| Prose claims (no hand-written counts or sizes, live paths, real subcommands, every CLI verb documented, the generated MCP tool tables match the catalog, every `docs/` page published or declared repo-facing) | ✓ `check-docs` | ✓ |
| DagNats doc pin vs go.mod | ✓ `check-dagnats-pin` | ✓ |
| SKILL.md frontmatter | ✓ | ✓ |
| install.sh bootstrap contract | ✓ | ✓ |
| MCP tool tests (`cmd/gogogo-mcp` shares the root module; scoped for a fast, readable failure) | ✓ `check-mcp` | ✓ (same script; also a pre-commit hook on `cmd/gogogo-mcp/**`) |
| datastar-lint | ✓ | ✓ |
| vendored stelow skill matches its pin | ✓ `check-stelow-drift` | ✅ `stelow-drift.yml` (weekly, `--strict`) |
| the upgrade loop works against real releases | — | ✅ `update-loop.yml` (weekly: scaffold the previous tag, expect "behind" and the range) |
| a newer Go **patch** is published | — | ✅ `go-auto-bump.yml` (weekly: opens a reviewed PR moving every pin; patch only — `make go-bump` by hand) |
| a newer **Pele** release is published (the Mermaid renderer behind chat diagrams) | ✓ `pele_version_test.go` (the bundle's banner and the constant must agree) | ✅ `pele-auto-bump.yml` (weekly: opens a reviewed PR with the bundle, the pin and the measured sizes — `scripts/bump-pele.sh`) |
| `uses:` action pins move | — | ✅ Dependabot (weekly, one grouped PR; the Go pin is not its business, see below) |
| golangci-lint (the `.golangci.yml` set) | ✓ | ✓ |
| govulncheck (dependency CVEs; the scanner itself is pinned in `go.mod`) | ✅ pre-push hook | ✓ |
| deadcode (advisory, host-gated + version-pinned) | ✅ pre-push hook | ✓ |
| `go test -race` (parallel, sharded by measured cost; on a **PR** only the affected packages **and their transitive dependents**) | ✓ full suite | ✓ full on master, scoped on a PR |
| build + **binary** boot smoke | ✓ | ✓ |
| browser smoke (Playwright) | ✓ | ✓ |
| whiteboard client (Playwright, canvas pixels) | ✓ | ✓ |
| SSE verified-open (slow-init opener proof) | ✓ | ✓ |

CI runs the rows above as `checks` | `race` (three shards) | `browser` (two
groups) | `prod-image`, plus `site` — wall-clock is the slowest job, not the
sum. A `ci` aggregator job (green only when all are) preserves the single status
context branch protection requires.

The `race` shards are not a count-based split: `scripts/ci-shard-packages.sh`
carries the measured per-package cost and gives an outlier its own shard before
bin-packing the rest, because `internal/installer` alone saturated the runner.
The package scope comes from the `changes` job: a PR runs the touched packages
plus their transitive dependents (test imports included), and a merged commit
runs everything, so a selection error cannot accumulate across PRs. An
unclassifiable changeset fails open to the full list. Numbers and the rejected
alternatives: `docs/research/ci-wall-clock-2026-10.md`.

A `workflow_dispatch` input exists for the paths a normal PR cannot reach:
`scope_packages` runs an explicit list instead of the classified scope, which is
how the narrow branch is verified and how a single shard's scope is reproduced
without opening a PR:

```
gh workflow run ci.yml --ref <branch> -f scope_packages="internal/queue features/notes/handlers"
```

### The nightly run, and why the workflow has a `schedule` trigger

`ci.yml` also runs on `schedule` (04:17 UTC daily). It is the same workflow,
unselected: a scheduled run is not a `pull_request`, so the `changes` job takes
its `kind=all` branch and every package runs. Nothing is cheaper for PRs
because of it.

What it adds is a **detector for the failures that only look green**. Test
selection, cache hits and cost tables are all optimizations that fail silently:
a cache that stopped hitting makes a run slower, never red; a stale per-package
cost entry makes the shard split unbalanced, never wrong; a gate whose
capability record drifted keeps passing because nothing it reads changed. A
nightly full run re-derives all of it against the actual tree, on a day nobody
is waiting.

A failure here is a finding, not a broken `main`: the scheduled run does not
report the required `ci` status, so it cannot block a merge. Treat it the way
`stelow-drift.yml` and `update-loop.yml` are treated — a weekly/daily signal
that something drifted, opened as a normal PR.

### The flake budget: what the retry policy is judged on

The browser suites retry a failed test once and mark it flaky; the race suite
deliberately never retries (a retried race failure hides the bug it exists to
find). That policy is only honest if the rate is measured — a retry that turns
red into green is exactly how a chronically flaky test survives, because nobody
ever sees the red.

    bin/flake-budget.sh                  # last 30 CI runs on master
    bin/flake-budget.sh --limit 60       # a longer window
    bin/flake-budget.sh --branch <ref>   # another ref
    bin/ci-wall-clock.sh --steps         # where the last run actually spent time

It prints the share of runs that had a failing job before going green, against
a **budget of 1%**, and names the jobs that appear inside otherwise-green runs.
A job that shows up twice or more is the quarantine candidate: mark it
known-flaky, keep running it, give it an owner and a date. One occurrence may
be infrastructure — read the log before calling it flaky, and preserve the
first failure's log, seed, shard and timing before retrying, because a retry
destroys the only evidence of what actually happened.

It does not gate, on purpose: a threshold in CI would go red for a reason that
has nothing to do with the change under test, which is the failure mode the
budget exists to prevent. Read it monthly, or when a run felt slow and retried.

### The cache budget, and why only the default branch writes

GitHub caps a repository at **10 GB** of Actions cache and evicts repo-wide by
last access. Measured 2026-11-19 (`gh api repos/<owner>/<repo>/actions/caches`):
23 entries, **11.0 GB** — already over the cap — of which 15 entries and
**8.0 GB belonged to a single pull-request merge ref**, because
`actions/cache@v6` saved from every ref that ran the key. That is the shape the
restore/save split below fixes: one generation per key shape instead of one per
open PR, so the budget is spent on entries that every run can use.

**A correction, kept because it is the more useful lesson.** The first reading of
this was that eviction had cost a browser job **113s** refetching Chromium (run
44551759905) against **11s** on a warm cache (run 44556795112). Reading the job
logs, both runs were a **cache HIT** for the browser binary. The 102s difference
was the apt phase of `bin/install-browser.sh`, which ran unconditionally:
`Fetched 4283 kB in 9s` followed by a second 21.9 MB fetch, against `Fetched
21.9 MB in 1s` when the mirrors were warm. The cache was never involved.

So that script now probes the launch FIRST and installs system dependencies only
when the probe fails, which is the ordering that makes the common case free. Two
measurements, two different causes, and only the second one was real — which is
why the job log is the source and the cache listing is only a hint.

So every cache in `ci.yml` is a **restore/save pair**: `actions/cache/restore`
runs everywhere (restoring from any ref is the point of a cache), and
`actions/cache/save` is gated on `github.ref == 'refs/heads/master'`. The budget
stays at one generation per key shape instead of one per open PR. The Go
toolchain tarball is cached the same way through `GO_TARBALL_CACHE`, and
`bin/install-go-toolchain.sh` re-checks it with `gzip -t` before extracting, so a
truncated entry costs a download rather than a broken toolchain.

### What is in the 11 GB, and the four fixes that did not survive measurement

The budget is spent like this (measured 2026-11-19, 39 entries, 11.04 GB):

| size | entries | what |
|------|---------|------|
| 6.54 GB | 14 | `go-build` — GOCACHE, one entry per job **and shard** |
| 2.19 GB | 3 | `go-mod-dl` — the module zips |
| 1.26 GB | 17 | `buildkit-blob` — docker layers pushed by `deploy.yml` |
| 0.69 GB | 1 | `setup-go` — from `release.yml`, not this workflow |
| 0.29 GB | 1 | `playwright` — Chromium, shared by both browser legs |

One fix came out of it: **`cache-to: type=gha,mode=min`** in `deploy.yml` (was
`mode=max`). `mode=max` stores every intermediate layer, including ones only a
from-scratch rebuild would reuse; `mode=min` keeps the final image's layers,
which is what the next build of the same Dockerfile pulls.

Four other candidate fixes were **measured and rejected**. They are recorded
because each looked obviously right and would have cost something real:

1. **Unify `go-build` into one cache those jobs share.** Local measurement: a
   unified cache is 1 513 MB against 1 927 MB summed for the per-job ones, so
   "−21%" looked free. Compiling the actual `-race` workload into it proved the
   opposite: **2 636 MB**, because `-race` artifacts are instrumented builds and
do not share objects with the non-race ones — a unified cache has to hold both
   sets. It was also **5.4× slower per shard** (12.0s vs 2.2s) because a
   dedicated shard cache already contains exactly the instrumented objects. Keep
   the per-job keys.
2. **Delete the `restore-keys` prefixes** to stop duplicate generations. The
   prefixes are what make a `go.sum` change cheap; without them every commit is
   an exact-key miss. The docs are explicit that `cache-hit` is `true` **only**
   on an exact match, so a prefix match already reports `false` and the save
   still runs — the duplicate generation is the *expected* cost of the prefix,
   not a bug in it. `go.sum` moved in **40 of the last 40 commits** here, so
   removing the prefix would mean a cold cache every run.
3. **Prune the cache by mtime before saving** (the suggestion in golang/go
   #69879, whose proposal to make the trim cutoff configurable was *declined*).
   Measured: `find <GOCACHE> -type f -mmin +90 -delete` freed **0 MB**, because
   everything in a CI cache is fresh from the run that is saving it.
4. **`GOCACHEPROG`** (Go 1.24+, entry-by-entry remote cache) is the structurally
   correct answer and needs a server the repo does not run; it is a separate
   project, not a workflow edit.

The remaining 1.66 GB of obvious waste — `checks-0` holding two generations
(1223 + 1271 MB) and `race-1` two more (438 + 438) — is the prefix doing its job
across a `go.sum` bump. It ages out when the generation stops matching, which
is why the budget has to be judged over weeks rather than at one instant.

Do not "fix" a slow job by widening the save condition. A cache that writes
from every ref and an eviction that follows is the expensive shape: it costs the
refetch it was meant to avoid, plus the runs that raced for space.

Those four skip on a **docs-only changeset** — a `changeset` job reads the PR
(`base...HEAD`) or push (`before...after`) diff and marks it when every path is
`docs/`, `site/`, `*.md`, `LICENSE`, `.stelow/` or `.gitignore`; an unreadable
diff (force-push, shallow history, dispatch) fails **open** to a full run. A
trigger-level `paths-ignore` would be cheaper but skips the workflow entirely,
leaving the required `ci` status pending forever on a docs PR — the classifier
always runs, so `ci` always reports. Each job provisions its own toolchain and
deps (`bin/check-ci-deps.sh`, pre-commit + CI, fails a job that uses
`node`/`go` without installing them first).

**Two jobs, not one, on purpose.** `changeset` (checkout + `git diff`) answers
"is this diff inert?" in ~5s, and that boolean is all `checks`, `browser`,
`prod-image` and `race`'s skip condition need. `changes` adds the pinned
toolchain, a restored module cache and `go list` to answer the SECOND question —
which packages `race` must run — and measures 27s. They were one job, so every
gate paid that 27s of serial prefix for a boolean that was ready at 5s, and the
wall's critical path was `prod-image` instead of a race shard. Splitting them
moves the five non-Go gates from `+32s` to `+6s` and takes ~15s off every PR
(measured on run 44142988832's job durations, not estimated). Only `race` reads
`changes.outputs.pkgs`.

**Re-measure the cost table before trusting the shard split.**
`scripts/ci-shard-packages.sh` carries measured per-package wall-times, and a
stale entry is an efficiency bug, not a correctness one: the union of shards is
always exactly `web-packages.sh`, but `internal/installer` kept its 2026-10-29
cost of 360s long after the fixture was narrowed, so the packer reserved a whole
runner for a package that now measures 6s — a 60x error that survived eight
consecutive runs. The cost table below is refreshed from
`gh run view <id> --log | grep -E '^race.*ok'`; `OUTLIER` is the threshold at
which a package is worth a runner of its own, and it is only correct relative to
the current maximum (64s today, so 60, not the 300 that matched a 360s outlier).

`make ci-local` runs the same set locally, cheap steps first, then the
race+browser fan-out (`scripts/ci-fanout.sh`).

One gate in `ci-local` is slower than the rest and still earns its place:
`make test-scaffold-chain` (also a step in the `checks` job) scaffolds twice —
the template's binary produces a product, the product's OWN binary produces the
next one — and proves the second hop keeps the product's identity. That hop is
the one no other test reaches, and it is where a fork silently renamed nothing,
cloned the template's org, and advertised the previous binary in its own
`AGENTS.md`. It is not in `ci-local-fast`: two build proofs cost about a minute,
and the identity only changes when the installer, the scaffold prompt or the
rename do.

The docs site BUILD is not part of `ci-local`: `pages.yml` builds `site/`
(including the generated search index under `site/search/`) and runs
`scripts/site-layout.test.mjs` before publishing. The site CHECKS are, because
they are read-only and cost ~0.2 s: `ci-local-fast` runs `site-check`, which
verifies the manifest, every internal link and anchor, and
`node site/build-landing.mjs --check` — the landing's unit and gate catalog
against `docs/unit-contracts.md` and `.lefthook.yml`. That last check exists
because a stale `site/index.html` used to reach CI whenever a hook was added
after the page was generated. `docsite-drift.yml` checks the
vendored engine against `site/UPSTREAM_SHA` on PRs that touch it and weekly;
`docsite-bump.yml` opens the bump PR.

**The generated-artifact checks are the ones that regressed.** The generated
`_templ.go` files and the committed CSS bundle (`app.min.css`) are *committed and
embedded in the binary*. Both CIs used to only **generate**
them and then run every subsequent step against the freshly generated output —
so a `.templ` (or `input.css`) edit whose regeneration was never committed
passed lint, passed tests, passed both smoke tests, and shipped a binary
rendering stale markup. Both now regenerate and **diff**, which is exactly what
`css-check` does.

**A second bundle slipped through that same hole once.** CI and
`bin/check-css.sh` built one artifact while their diff named two, so the second
was compared against itself — empty by construction, green however stale the
committed file was. `make css-check` builds what it diffs, and CI and the hook
call that same target: one command, no drift.

This is deterministic, and that matters: the reason `actions/cache` is disabled
in this repo (`skip-cache: true`, `cache: false`) is a *real, documented*
history of flaky tar restores (`/usr/bin/tar` exit 2) aborting jobs — see
commit `d50d0fb`, where caching was turned off deliberately rather than
overlooked. Do not re-enable it to save seconds without reading that first;
the CSS/templ diffs above add no such risk because they compare text, not
extract archives.

The one exception is narrow and deliberate (`docs/research/ci-wall-clock-2026-10.md`):
explicit `actions/cache@v4` steps cache the Go build cache and the Playwright
browsers, each with `continue-on-error`. The flake above was in the *internal*
caches of `setup-go`/`setup-node`/`golangci-lint-action`, which use the system
`tar` and are still off; the explicit action restores through its own JS/zstd
client, and a failed restore falls through to a cold build instead of failing
the job. A repeated restore failure means deleting the step, not debugging CI
infrastructure. The Go *module* cache is not cached at all: restoring it was
measured at 23-33s per job against 16-29s of saved download, i.e. break-even to
a loss, while the build cache cut the installer shard's test step 360s→134-186s,
`browser`'s build 76s→7s and `checks`' lint 107s→18s.

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

## Proving a guard can fail (red-proof)

A test that cannot fail reports coverage it does not have. The guards that keep
the revamp honest — and the UX decisions it made — are each proven by mutation
before being trusted:

```bash
bash bin/red-proof-ui.sh      # 12 cases: mutate, require red, restore, require green
```

Cases: a removed surface named in a doc / in code / in an extensionless build
file, a dead `data-variant` back on an element, a catalog theme the bundle does
not compile (and the reverse), a duplicate catalog entry, the `#todo-list` patch
root renamed, an admin-only module exposed to viewers, the release knob flipped
back to major, a manifest hand-bumped to `1.0.0`, and a template that owns
`<html>` without the theme wiring. A `.templ` mutation re-runs
`go tool templ generate` first — the server renders from the generated
`*_templ.go`, so without it the mutation is invisible and the guard looks blind.
That failure mode is real: two cases were no-ops on their first run, and the
guards were fine.

Two guards **were** blind and got rewritten:
`TestGroupedHidesAdminModulesFromNonAdmins` checked the same `Admin` flag the
sidebar filters by (deleting the flag deleted the assertion too — it now asserts
against an explicit admin-only set, both directions), and the page-contract
mutation had been hitting a doc comment quoting `#todo-list` instead of the
element carrying it.

The mobile drawer has the same treatment inside the smoke suite: at 390×844 the
rail must start off-canvas, open from the module header, and close on Escape.
Remove `data-attr:checked="$railOpen"` from the shell's drawer toggle and
`smoke.mjs` goes red on that assertion — the rail stops following the state.
The desktop collapse is checked the same way, and so is the unread marker that
moves with it: expanded the count is an `indicator-item` on the rail label,
collapsed it is a dot on the icon and the number is gone. Delete
`RailInboxDot` and the suite goes red on "collapsing moves the unread marker
onto the icon" — a collapsed rail that silently loses the unread fact looks
exactly like one that is working.

Release PRs get it from the workflow itself: the `release-guard` job fails when
the proposed version leaves 0.x (a `1.0.0` title, or a manifest somebody bumped
by hand), so the one PR nobody should merge by reflex is red before it is
mergeable.

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
| `pre-commit` (parallel) | file-sizes · fmt-gofumpt · mod-tidy · govulncheck · ci-deps · install-sh · ci-scripts · class-integrity · daisyui-lint · prompts · flatdeps-lint · product-boundary · typed-states · shape-domains · test-value · docs-claims · secrets-env · rbac-policy · dagnats-pin · secrets-scan · skill-frontmatter · datastar-lint · api-routes · receipt-hashes · check-locales · mcp-tests · registry-conformance · generated-templ · css-check · no-jsx · golangci-lint (host-aware, `scripts/lint-safe.sh`) · agents-md-staleness · stelow-drift (advisory) · daisyui-drift (advisory) · docs-staleness (advisory) | every commit; glob-filtered jobs skip when nothing relevant is staged |
| `pre-push` | govulncheck · deadcode | every push |
| `post-merge` | regen-assets (templ + css when templ/go/css changed) | after pulls/merges |

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

External tools are **version-pinned in `go.mod`** (the `tool` directive, used
by `templ`, `deadcode` and `govulncheck`) and run through `go tool`, never
`go install @latest`. That keeps a scan from disagreeing with the toolchain
about which module version it is type-checking — the mismatch that made
`deadcode` report type-check errors as "dead code found" — and it keeps the
binary a security gate runs from being decided by whoever published last. A
`govulncheck` fetched with `@latest` was the one gate that broke that rule;
`internal/gate_tools_test.go` now enforces both halves (an install with a
floating ref, and a bare `govulncheck` resolved from `PATH`). Add another tool
the same way: `go get -tool <pkg>@<version>`, then call it as `go tool <name>`.

### Raising the Go toolchain pin

The pin is not one version string: `go.mod`, `bin/install-go-toolchain.sh`, both
Dockerfiles, the coding-standards skill,
`docs/troubleshooting.md` and the pin test's own table all name it, and
`internal/toolchain_pin_test.go` fails when one of them lags. So the bump is
one command, never a hand edit:

```bash
make go-bump            # newest patch of the tree's current minor line
make go-bump DRY=1      # report the files that would move, write nothing
make go-bump TO=1.27.4  # an explicit target
```

`cmd/bump-go-toolchain` reads `go.dev/dl` (no token, no API budget), rewrites
every occurrence as a WHOLE version token (so `go1.27.2` in an example and
`1.27.30` in a neighbour survive), and stops when a file of the surface is
missing, no longer carries the version, or moved fewer occurrences than it
had — a partial rewrite is exactly what the pin test cannot see.

Two safety properties, both about not leaving a half-bumped tree behind: the
whole surface is validated **before the first byte is written** (so "it failed"
means "nothing changed"), and an explicit `TO=` is confirmed against
`go.dev/dl` with a HEAD on that release's source tarball, so a typo cannot pin
the tree to a version nobody published.

Two rules, both deliberate: **patch only** (a minor bump changes language
semantics and the doc claims that speak at minor granularity — a human
decision), and **a PR, not a commit** (the toolchain decides what bytes ship).
`.github/workflows/go-auto-bump.yml` runs the same command weekly and opens
that PR — then dispatches `ci.yml` on the branch itself, because a PR opened
with the workflow's own `GITHUB_TOKEN` triggers no `pull_request` run and `ci`
is the required status on `master`. `make go-bump` is the human-in-the-loop
version.

Dependabot cannot do this: its `gomod` ecosystem does not update the `go`
directive (dependabot-core#9057, still open; Renovate documents the same
default). `.github/dependabot.yml` therefore covers only the `uses:` action
pins.

> If the pre-commit hook reports every job green but still exits non-zero, read
> the **middle** of the output, not the summary — the jobs run in parallel and
> the summary block prints last. A `go get` that leaves stale `go.sum` lines is
> the usual culprit; `go mod tidy && git add go.mod go.sum` fixes it.

## Related

- [Code quality](code-quality.md) — the linter set and how to run it scoped.
- [Troubleshooting](troubleshooting.md) — when the gate is green but behavior isn't.
- [Deploy](deploy.md) — what happens after the push.