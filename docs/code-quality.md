# Code quality

The template ships a strict `golangci-lint` configuration (33 linters)
designed to catch the kinds of mistakes LLMs make most often: unchecked errors,
insecure patterns, broken context propagation, resource leaks, and inconsistent
error wrapping. The goal is not to block development but to redirect agents
toward correct Go idioms automatically.

Rules the project learned by hand live in `rules/rules.go` and are loaded by
`gocritic`'s `ruleguard` check (see [Custom rules](#custom-rules-ruleguard)
below) — prose guidance that a linter can enforce is enforced instead.

The rules an **agent** is meant to load — Go idioms, concurrency, testing, the
exception-to-go gate — live in
[`skills/gogogo-coding-standards`](https://github.com/calionauta/gogogo/tree/master/skills/gogogo-coding-standards)
(install with `npx skills add calionauta/gogogo`). Its frontmatter is validated
by `make check-skill-frontmatter`, because the skill *host* parses that YAML and
this repo's build never does.

## What the linters enforce

| Category | Linters | What they catch |
|----------|---------|-----------------|
| Correctness | `govet`, `staticcheck`, `errcheck`, `ineffassign`, `unused` | Shadowed variables, dead code, unchecked returns |
| Error handling | `errorlint`, `nilerr`, `gosec` | Wrong `%w` formatting, returning nil inside an error path, hardcoded credentials |
| Resource safety | `bodyclose`, `noctx`, `fatcontext`, `prealloc` | HTTP bodies and contexts not closed or propagated, nested `context.With*` inside loops, slices grown without a capacity hint |
| Cancellation | `forbidigo` (no bare `time.Sleep`), `contextcheck` | A wait that cannot be interrupted: `time.Sleep` holds its goroutine — and any `wg.Wait()` draining it at shutdown — for its full duration. Three real sites shipped this way (the queue worker's 1s receive-error backoff, the onboarding-greet pause, and its retry loop); all three are now `select` on a context. Test files are exempt, because asserting an ABSENCE sometimes needs a fixed window. |
| Test quality | `thelper`, `testifylint`, `sloglint`, `containedctx` | Missing `t.Helper()`, `assert` vs `require` misuse, context embedded in structs |
| Complexity | `gocyclo`, `gocognit`, `funlen` | Functions too long or too nested to hold in working memory |
| YAGNI | `unparam` | A parameter that is constant (or a result that is unused) at every call site: a constant wearing a parameter’s clothes, or a return nobody reads. Removing it is the fix — a placeholder `_` return is not. |
| Style | `revive`, `gocritic`, `tagliatelle`, `goconst`, `mnd`, `dupl`, `lll`, `modernize`, `perfsprint`, `usestdlibvars`, `nolintlint` | Non-idiomatic patterns, magic numbers, duplicated code, long lines, slow `fmt.Sprintf`, literal `"200"`/`"GET"` instead of `http.StatusOK`/`http.MethodGet`, and ill-formed or unexplained `//nolint` directives |
| Formatting | `gofumpt` + `goimports` (formatters, not linters) | Compulsory consistent layout and import ordering |

The configuration lives in `.golangci.yml` at the project root — read it if you
need to understand what each linter expects.

**If a lint forces you to restructure code, that is usually a sign the original
approach had a deeper issue.**

## Running each layer

| Command | What it checks |
|---|---|
| `make lint` | `go vet` + `golangci-lint` (33 linters) over the web packages — `scripts/web-packages.sh` excludes `cmd/desktop` and `cmd/gui` |
| `make lint-safe` | Host-aware lint: sizes the run to free RAM/cores, scopes to changed packages when memory is tight, caps the run in a cgroup (`scripts/lint-safe.sh`). Prefer over `make lint` on a shared host. |
| `make datastar-lint` | Datastar-specific anti-patterns in `.templ` files |
| `make fmt` | `gofumpt` + `goimports` formatting only |
| `make ci-local` | Full local gate, identical to CI: templ → datastar-lint → css-check → check-scope → check-skill-frontmatter → install-sh-guard → check-sizes → golangci-lint → race tests → build → binary boot smoke → Playwright smoke |
| `make check-stelow-drift` | Verifies the vendored `stelow-workflow-coding-standards` copy still matches its pinned upstream commit AND that upstream `main` has not moved. Advisory in pre-commit (never blocks on a network call); `--strict` here and in the weekly `stelow-drift` workflow. Update with `scripts/refresh-stelow.sh` — never by hand. |

We deliberately keep `gofumpt` and `goimports` as **formatters** (not linters)
so `golangci-lint run` never auto-formats your files — formatting is a separate
explicit step. `ci-local` uses `golangci-lint` as the authoritative formatter
gate rather than the standalone `gofumpt` binary, which can be a newer release
than the one golangci-lint bundles and would otherwise produce false-positive
listings.

## Scope your lint runs

`golangci-lint run ./...` on the whole repo is ~10× slower than scoping to the
packages you touched. Always scope:

```bash
golangci-lint run ./features/todo/... ./internal/server/...
```

On a host that also runs other work (agents, production, a co-tenant daemon),
use `make lint-safe` instead. It measures free RAM and cores at run time, runs
the full repo only when there is headroom, otherwise scopes to the changed
packages, and caps the run in a cgroup. It also **never** runs
`golangci-lint cache clean` — the cache is what keeps a run cheap, and a cold
run re-type-checks the whole module and spikes memory.

## Custom rules (ruleguard)

Some footguns are project-specific — no off-the-shelf linter covers them, so
instructing an agent about them in prose is the only alternative. Instead they
are encoded in **`rules/rules.go`** and loaded by `gocritic`'s built-in
`ruleguard` engine (`.golangci.yml` →
`linters.settings.gocritic.settings.ruleguard.rules`). CI fails on a hit, so
the rule is enforced rather than merely documented.

The current rules flag `<-time.After(...)` used directly as a `select` case,
`for range <ticker>.C` loops with no exit, 500 bodies concatenating
`err.Error()`, and the `sqlite` driver name — each documents its own bitten
precedent and reference fix inline. Matching narrow shapes (the *receive*
form of `time.After`, the 500-only error text) keeps legitimate neighbours
unflagged.

Structural boundaries live one level up, in **`cmd/check-flatdeps`**
(`make check-flatdeps`, same gate in CI and pre-commit): no banned framework
in the dependency closure, no cross-feature domain imports (shared UI leaves
`authviews`, `sounds`, `genui/components` excepted), and `sql.Open` only in
owner packages (`internal/db/`, `internal/queue/`, `features/credits/`).

Secret plumbing has its own gate, **`cmd/check-secrets-env`**
(`make check-secrets-env`, same gate in CI and pre-commit):
`os.Getenv`/`LookupEnv`/`Setenv` with secret-shaped names (`*_KEY`,
`*_TOKEN`, `*_SECRET`, `*_PASSWORD`, …) may only run in owner files —
`config/` (single source of truth), `internal/secrets/` (age loader),
and the narrowly reasoned call sites listed in the tool. Values stay
gitleaks' job; this tool guards *sites*, so a new credential either
lands in config (documented, masked on `/config`, age-sourced) or fails
with the file and line named.

Adding a rule:

- Patterns are **expressions**, not statements — `select { case ... }` does not
  parse. Match the call and constrain with `.Where()` when context matters.
- The file needs the `//go:build ruleguard` tag; `github.com/quasilyte/go-ruleguard/dsl`
  is a build-time dependency declared in `go.mod`.
- **Never duplicate a stock linter** — `perfsprint` already covers
  `fmt.Sprintf` → `strconv`, so a rule for it would be pure noise.
- Keep the set small. A rule with false positives teaches people to ignore the
  linter. Verify a new rule against the real tree (`golangci-lint run ./...`)
  before committing it.

Full-repo lint is reserved for `make lint` and `make ci-local`; on a
memory-tight host prefer `make lint-safe`, which decides full vs scoped from
the machine's free RAM.

## Git hooks

The lefthook hooks (`make setup`) run `gofumpt`, `goimports`, `datastar-lint`, a
CSS staleness check, `go mod tidy`, the SCOPE annotation linter, and a
host-aware `golangci-lint` (`scripts/lint-safe.sh`) on every commit — so
formatting and lint violations never reach the remote. Jobs are glob-filtered
(only run when matching files are staged) and executed in parallel.

Run **`make signoff`** for the full gate before pushing. See
[Local CI](local-ci.md) for the tier ladder and why it beats waiting on remote
runners.

## Datastar-specific rules

`make datastar-lint` catches mistakes `golangci-lint` cannot see, because they
live in `.templ` markup rather than Go:

- A `PatchElements` whose top-level element lacks an `id` and a
  `WithSelector` selector throws `PatchElementsNoTargetsFound` in the client.
  Pair `internal/datastar.RenderAndPatch` with an explicit selector.
- Prefer Datastar attributes (`data-on:*`, signals, expressions,
  `__window`/`__document` modifiers) over vanilla JS for client-side logic.
  Inline JS only when unavoidable, kept adjacent to the markup so the behavior
  stays local.
- `<html data-signals>` is JSON, and the pre-paint theme bootstrap
  (`authviews.ThemeHead`) MERGES its `theme` key into it — never writes a fresh
  object, which drops every seed on the page before Datastar reads the
  attribute. A page that seeds state there (`review_pending`, `genui_pending`,
  `chat_thinking`) must emit valid JSON for the merge to work.
- **A page that renders the navbar must wire `$theme`, or the toggle looks
  broken.** The button lives in `Navbar`, so any page that includes the navbar
  shows a toggle that runs — it flips `$theme` and calls `Theme.syncIcons`.
  But `$theme` only *does* anything when `<html>` carries the three bindings,
  and without them nothing observes the signal: the icon sync hides both icons
  (the CSS reveals the moon only under `html.dark`) and the theme never
  changes. Use the shared helper rather than copying the attributes:

  ```templ
  <html lang="en" { authviews.ThemeRootAttrs("")... }>
  <!-- or, keeping the page's own seeds: -->
  <html lang="en" { authviews.ThemeRootAttrs(MarshalSignals(signals))... }>
  ```

  `ThemeRootAttrs` supplies `data-signals` + `data-attr:data-theme` +
  `data-class:dark`. The copy-per-page form is what diverged: three pages
  (`/config`, `/room`, the landing page) shipped the navbar with none of the
  three, and the toggle silently did nothing. `features/authviews/theme_test.go`
  scans for the wiring and fails on a new page that forgets it.
- **`ThemeHead` is also what loads the iconify component.** `<iconify-icon>` is
  a web component: without `/static/iconify-icon.min.js` the tag lays out at
  0×0, so every icon — the theme toggle's sun/moon and the sound toggle — is
  simply not there, and a toggle that swaps its icon looks like it vanishes.
  The script lives in `ThemeHead` (one owner) rather than in each page; the
  navbar uses iconify icons, so a page that renders the navbar and not
  `ThemeHead` shows invisible icons. Eight pages had each copied the tag and
  three had forgotten it — the same "every page must remember" shape that let
  the theme bindings drift.
- A signal that never reached the store is read as `""`, and Datastar's
  `data-attr:disabled` sets the attribute for `""` — so a wiped seed is a
  control born disabled. Only its own submit handler clears it, so the page
  cannot recover. A stream handshake that re-seeds the signal (Ask and Chat
  merge `pending: false` on connect) can hide the loss; don't rely on it.
- **A `data-bind` signal is client-owned: never write it from a server merge.**
  The bound element is the source of truth while the user types, and the
  server's copy always lags (a value only arrives after a debounced
  round-trip). A background merge — stream handshake, record-event re-list,
  workflow re-list, mutation re-list — therefore overwrites live input, and the
  debounced request then reads the clobbered value and the feature silently does
  nothing (lived bug: `searchQuery` was written from six call sites — the
  stream handshake, both list endpoints, the mutation re-list, and the `/search`
  response echoing its own `?q=` — and the search box lost its text mid-typing;
  the smoke caught it only intermittently). The composers carried the same
  write: the create response cleared `newTitle` and both ask acks cleared
  `chatPrompt`/`genuiPrompt`, erasing text typed while a slow answer was still
  in flight. The counts are `datastar-lint`'s, not a hand tally: 9 findings on
  the pre-fix tree (7 mentioning `$searchQuery`, one of which was the missing
  `omitempty` on the struct), and **0 after**.
  The struct field must also be `omitempty` **or excluded**: without
  `omitempty` the zero value still marshals (`"x":""`), so removing the field
  from the merge *call* does not stop the write — which is why the first fix
  attempt was a no-op. `CLIENT_BOUND_SIGNAL_NO_OMITEMPTY` (error) and
  `CLIENT_BOUND_SIGNAL_WRITTEN` (error) gate both halves, and since
  datastar-lint v0.16.0 the write check reads **three doors** — a Go merge, a
  `data-signals:key`, a `data-signals={obj}` — because a server-rendered
  signals attribute re-applies on every morph.
  **The zero finding is not a suppression.** `Signals.SearchQuery` is
  `json:"-"`, so it never reaches `data-signals`; the input seeds
  `$searchQuery` from its own `value=` (bind.ts merges with `{ifMissing:
  true}`). The seed therefore never writes a client-owned signal, and there is
  nothing to silence. Reach for `json:"-"` + `value=` before reaching for a
  suppression: the lint reports `SUPPRESS_UNUSED` the moment a write is gone,
  so an escape hatch cannot outlive its subject. This repo goes further:
  `.datastar-lint.yaml` lists `CLIENT_BOUND_SIGNAL_WRITTEN` under
  `suppressions.forbidden`, so a `// datastar-lint:ignore` for it is rejected
  outright (`SUPPRESS_BLOCKED`) even with a reason. The lock is safe precisely
  because the idiom above needs no suppression — a reviewer cannot be talked
  past a comment the linter refuses to accept.
- **A response that echoes the request's own signal is the same clobber.**
  `/search` derives no display value from `?q=` for that reason: the box is
  seeded once, by the input's own `value=` on first render, and every later
  response patches rows only. A response that re-emits what its request carried lands
  late and re-imposes an older value (`?q=kes` overwriting "kestrel"), so the
  rule is not "don't write it in background paths" but "don't write it in any
  response".
- **A tab/scope click carries no search of its own**, so its URL must append
  `&q=$searchQuery`: the list handler reads a missing `q` as "clear the stored
  search", and omitting it drops the active search while the box still shows
  the text.

The lint runs with `-only-errors` in CI, which means only ERROR-severity
findings ever gate. That is why the bound-signal write is ERROR: as a warning it
was invisible to both gates, and the bug it describes shipped with the rule
already in the tree. Custom attributes (the cuelume `data-cuelume-*` set,
Basecoat's `data-size`/`data-variant`) are noise at that severity, so they live
in `.datastar-lint.yaml` under `attributes.allowed` — each verified to have a
consumer before listing, since an entry also hides a misspelling of a real
attribute.

## Concurrency and resources

These are the mistakes `go test -race` alone does not catch (a leak is not a
race). Each has bitten this template; each now has a rule and, where useful, a
linter or a test.

**Background goroutines need a shutdown path.** A `go func()` that loops on
`time.NewTicker` with no exit case survives process shutdown and leaks for the
life of the parent — and if it is started once per request, it leaks per
request. Every long-lived loop selects on a cancellation signal:

```go
for {
    select {
    case <-ctx.Done():  // required
        return
    case <-ticker.C:
        work(ctx)       // pass ctx, not context.Background()
    }
}
```

- Bind the lifetime at WIRING time, not per request. In the composition root the
  shape is `ctx, cancel := context.WithCancel(context.Background())` bound to the
  stack's shutdown and drained via the `stop` func `Serve` returns,
  then hand `ctx` to the worker. See `bootCredits` in `internal/server/chi.go`,
  `AttachJetStream`, and `internal/queue/` (worker stop path).
- **Never** store `context.Context` in a struct: the `containedctx` linter
  rejects it, and it is the usual cause of a context outliving its scope. Pass
  `ctx` as a parameter or hold a `done chan struct{}` + `sync.Once` (see
  `internal/queue.WorkerPool`, `OnboardingHandler.shutdown`).
- A loop that must survive the client (a durable workflow poll) still must NOT
  use `c.Request.Context()`. Derive from `context.Background()` and cancel on
  app termination, as `OnboardingHandler` does.
- `errgroup` is the right tool when fanning out N tasks that should fail
  together: `g, ctx := errgroup.WithContext(ctx)` then `g.Go(func() error {...})`.
  Do not hand-roll `WaitGroup` + error channel.

**Channels.** A buffer of 0 or 1 around a producer that must not block is a
deadlock waiting to happen; the SSE hub buffers per client
(`config.DefaultClientQueueSize`) and drops on a full channel rather than
blocking the worker pool (`internal/queue/ssehub.go`). Drop deliberately and
log it — never block a shared producer on one slow consumer.

**Test concurrency deterministically with `testing/synctest` (Go 1.24+).**
It runs the code in a bubble where `time` is virtual: a 1-minute ticker fires
instantly, and a leaked goroutine fails the test as a deadlock instead of
hanging CI. See `features/credits/settlement_synctest_test.go`:

```go
synctest.Test(t, func(t *testing.T) {
    go settlementLoop(ctx, time.Minute, drain)
    synctest.Sleep(time.Minute) // virtual — no real wait
    synctest.Wait()             // blocks until the bubble is quiescent
})
```

Use it for timers, tickers, and retry backoffs; use `-race` for data races.
`t.Context()` (Go 1.24+) is the default context in tests — it is cancelled at
cleanup, so it is already the right parent for a test goroutine.

**Assert leaks with `goleak`, never `runtime.NumGoroutine()`.** The count is
process-global and races sibling tests (it can even go *down* mid-test). Use
`defer goleak.VerifyNone(t, goleak.IgnoreCurrent())`, plus
`goleak.IgnoreTopFunction("database/sql.(*DB).connectionOpener")` when the test
opens a DB. See `features/credits/lifecycle_test.go`.

**Timeouts must exceed SQLite's `busy_timeout`.** The repo sets
`busy_timeout(10000)` on the DB DSN, so a writer blocks up to 10s for the lock.
A test or request timeout shorter than that cancels the request while the
database is still legitimately waiting — an intermittent `context deadline
exceeded` that looks like flakiness and is really lock contention. `busyTimeoutMS`
in `internal/db/db.go` is the single source for that value; keep any test HTTP
client's timeout above it (`internal/queue/dsn_test.go` exercises the contention
directly). Every SQLite DSN needs `busy_timeout`
+ `journal_mode(WAL)` **in the DSN**; a `PRAGMA` statement does not cover pooled
connections (`internal/queue/goqite.go` once omitted it).

**Allocating and formatting.** `strconv.Itoa`/`FormatInt` are several times
cheaper than `fmt.Sprintf` on the hot path — reach for `strconv` when the
argument is a single value, and reserve `fmt` for formatting that actually
needs verbs. Preallocate when the size is known: `make([]T, 0, n)` and
`make(map[K]V, n)`. Build strings in a loop with `strings.Builder`, never with
`+=` (each `+=` reallocates and copies the whole string). Do not chase struct
field alignment: `fieldalignment` is intentionally disabled, because padding
the struct hurts readability more than it saves on modern hardware.

**Confining file writes to a directory tree.** When writing into a path that
may come from an untrusted or user-controlled tree, a lexical check
(`filepath.Join` + `HasPrefix`) is not enough — a symlink planted inside the
tree passes it and the write escapes. Use `os.Root` (Go 1.24+):
`r, _ := os.OpenRoot(dir)` then `r.WriteFile(rel, ...)`, `r.MkdirAll(rel, ...)`.
Every operation is resolved against the directory handle and a symlink that
would leave the root is refused. See `internal/installer/tree.go`.

**Resolve the caller's path before deriving the root-relative name.** A
relative `--dir` (the installer's default is `./<name>`) makes every path the
caller builds CWD-relative, and `filepath.Rel` refuses to mix those with the
root's absolute path: each confined operation then fails with
`can't make X relative to Y` while the swallowed-error sites do nothing.

## File size limits

The pre-commit hook runs a `file-sizes` check. Large files are a smell in this
template specifically because they are usually a sign that a feature grew
monolithically — split by concern rather than raising the limit.

## Related

- [Local CI](local-ci.md) — the tier ladder and `make signoff`.
- [Scope taxonomy](scope-taxonomy.md) — the other structural linter
  (`check-scope`).