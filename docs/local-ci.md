# Local CI

Pushing to `master` triggers GitHub Actions: `ci.yml` runs the full gate, then
`deploy.yml` ships to production. Run that **exact gate on your own machine**
before pushing, so you don't wait on remote runners and don't push a broken
commit.

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
| **T2** lint scoped | `go vet` + `golangci-lint run <changed-glob>` + `make templ` / `make datastar-lint` (when `.templ` changed) | ~15–20s | Shadow, mnd, nolintlint, revive, staticcheck, Datastar attribute mistakes |
| **T3** tests scoped | `go test -race -count=1 <changed-pkg>` | ~5–30s | Race detector on tests, business logic |
| **T3.5** fast gate | `make ci-local-fast` | **~2–30s** | T1+T2 for the whole repo's cheap checks, plus race tests for **only the changed packages** (auto-detected) |
| **T4** full local gate | `make ci-local` | ~60–240s | Full pre-push check (= CI) |
| **T5** signoff local | `make signoff` (= T4 + `gh signoff -f`) | ~60–240s | Same as T4, plus it commits the verification to git |

### Why the full gate is slow, and the fast sibling

The full run is dominated by `go test -race`, and within it by
`features/todo` (a real PocketBase + goqite + SSE fixture per test, ~90s, over
60% of the suite). Everything else is cheap: templ ~1s, css-check ~5s,
check-scope <1s, lint ~10s, Playwright ~3s when cached.

`make ci-local-fast` runs the same cheap-but-decisive checks
(`templ`, `datastar-lint`, `css-check`, `check-scope`) plus **scoped** lint and
race tests, narrowed by `scripts/changed-packages.sh` to the packages your
diff actually touches. Measured: **~2s** for a CSS-only change, **~10s** for a
single-package Go change, versus ~240s full. It falls back to all packages when
a shared file moves (`go.mod`, `config/`, `db/`, `internal/capabilities/`) or
when nothing changed.

Use it while iterating; the full `ci-local` (and `make signoff`) remain the
authoritative pre-push gate — `ci-local-fast` does not replace CI coverage, it
just stops a CSS tweak from paying for the todo suite.

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

## When CI goes red

1. Read the failing log step (test, lint, css-check, build).
2. Reproduce locally with `make ci-local` — usually the same failure.
3. Fix and commit.
4. `make signoff` again — green means it would pass on a re-run.

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
| `pre-commit` (parallel) | file-sizes · fmt-gofumpt · mod-tidy · scope-lint · datastar-lint · css-check · golangci-lint · agents-md-staleness | every commit; glob-filtered jobs skip when nothing relevant is staged |
| `pre-push` | govulncheck · deadcode | every push |
| `post-merge` | regen-assets (templ + css-all when templ/go/css changed) | after pulls/merges |

> If the pre-commit hook reports every job green but still exits non-zero, read
> the **middle** of the output, not the summary — the jobs run in parallel and
> the summary block prints last. A `go get` that leaves stale `go.sum` lines is
> the usual culprit; `go mod tidy && git add go.mod go.sum` fixes it.

## Related

- [Code quality](code-quality.md) — the 31 linters and how to run them scoped.
- [Troubleshooting](troubleshooting.md) — when the gate is green but behavior isn't.
- [Deploy](deploy.md) — what happens after the push.