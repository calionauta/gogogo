# Code quality

The template ships a strict `golangci-lint` configuration (31 linters)
designed to catch the kinds of mistakes LLMs make most often: unchecked errors,
insecure patterns, broken context propagation, resource leaks, and inconsistent
error wrapping. The goal is not to block development but to redirect agents
toward correct Go idioms automatically.

Rules the project learned by hand live in `rules/rules.go` and are loaded by
`gocritic`'s `ruleguard` check (see [Custom rules](#custom-rules-ruleguard)
below) — prose guidance that a linter can enforce is enforced instead.

The rules an **agent** is meant to load — Go idioms, concurrency, testing, the
Zig gate — live in
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
| Test quality | `thelper`, `testifylint`, `sloglint`, `containedctx` | Missing `t.Helper()`, `assert` vs `require` misuse, context embedded in structs |
| Complexity | `gocyclo`, `gocognit`, `funlen` | Functions too long or too nested to hold in working memory |
| Style | `revive`, `gocritic`, `tagliatelle`, `goconst`, `dupl`, `lll`, `modernize`, `perfsprint`, `usestdlibvars` | Non-idiomatic patterns, magic numbers, duplicated code, long lines, slow `fmt.Sprintf`, literal `"200"`/`"GET"` instead of `http.StatusOK`/`http.MethodGet` |
| Formatting | `gofumpt` + `goimports` (formatters, not linters) | Compulsory consistent layout and import ordering |

The configuration lives in `.golangci.yml` at the project root — read it if you
need to understand what each linter expects.

**If a lint forces you to restructure code, that is usually a sign the original
approach had a deeper issue.**

## Running each layer

| Command | What it checks |
|---|---|
| `make lint` | `go vet` + `golangci-lint` (31 linters) over the web packages — `scripts/web-packages.sh` excludes `cmd/desktop` and `cmd/gui` |
| `make lint-safe` | Host-aware lint: sizes the run to free RAM/cores, scopes to changed packages when memory is tight, caps the run in a cgroup (`scripts/lint-safe.sh`). Prefer over `make lint` on a shared host. |
| `make datastar-lint` | Datastar-specific anti-patterns in `.templ` files |
| `make fmt` | `gofumpt` + `goimports` formatting only |
| `make ci-local` | Full local gate, identical to CI: templ → datastar-lint → css-check → check-scope → golangci-lint → race tests → build |

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
golangci-lint run ./features/todo/... ./router/...
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

The current rule flags `<-time.After(...)` used directly as a `select` case:
each evaluation allocates a `*time.Timer` that is not reclaimed until it fires,
so in a loop every iteration another case wins leaks a timer for its full
duration. The correct idiom is one `time.NewTimer` hoisted above the loop and
`Reset` each iteration (`internal/queue/workers.go` is the reference fix).
Matching the *receive* form rather than any `time.After` call keeps the
legitimate one-shot `timeout := time.After(d)` above a loop unflagged.

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

The lint runs with `-only-errors` in CI so intentional custom attributes (the
Morpheus skin's `data-neo-*` attributes, for example) do not fail the gate.
Add genuinely intentional attributes to `.datastar-lint.yaml` under
`attributes.allowed`.

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

- Bind the lifetime at WIRING time, not per request. In the router the shape
  is `ctx, cancel := context.WithCancel(context.Background())` +
  `se.App.OnTerminate().BindFunc(func(e *core.TerminateEvent) error { cancel(); return e.Next() })`,
  then hand `ctx` to the worker. See `router/collab_jetstream.go`,
  `router/realtime_jet.go`, and `router/credits.go`.
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
exceeded` that looks like flakiness and is really lock contention
(`features/todo/crud_test.go` uses 20s). Every SQLite DSN needs `busy_timeout`
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

## File size limits

The pre-commit hook runs a `file-sizes` check. Large files are a smell in this
template specifically because they are usually a sign that a feature grew
monolithically — split by concern rather than raising the limit.

## Related

- [Local CI](local-ci.md) — the tier ladder and `make signoff`.
- [Scope taxonomy](scope-taxonomy.md) — the other structural linter
  (`check-scope`).