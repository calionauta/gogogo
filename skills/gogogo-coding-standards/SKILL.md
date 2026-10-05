---
name: gogogo-coding-standards
description: Go backend standards for the gogogo template — PocketBase/SQLite, Templ, Datastar, NATS JetStream, goqite, slog, goroutines/channels/mutex/errgroup, context propagation, golangci-lint, race/synctest/goleak tests, datastar-lint on .templ, pprof/simd/Zig gates. Triggers when: editing .go files, spawning goroutines, creating channels, wiring context, running lint/tests, touching .templ, profiling, or proposing native code. Delegates universal principles to stelow-workflow-coding-standards.
---

# gogogo-coding-standards

Go + template rules only. Universal principles (KISS, DRY, LoB/SoC, YAGNI, sizes) live in `stelow-workflow-coding-standards` and are not repeated here.

- Source: https://github.com/calionauta/stelow/tree/main/skills/stelow-workflow-coding-standards
- Go override (copied, stable): max 100 lines/function, 500 lines/file. Universal default is 50/400.
- This skill covers only what stelow does not: Go idioms, gogogo gates, Datastar, perf, Zig gate.

## When to Use

Activate when: editing any `.go` file, spawning a goroutine, creating a channel, touching `context.Context`, running `golangci-lint`/`go test`, touching `.templ`, profiling, or proposing SIMD/Zig. Do not activate for copy writing, landing-page CSS, or release notes.

## Core Go Rules

1. Errors are values: handle at call site, wrap with `%w`. See `errorlint`, `nilerr` in `.golangci.yml`.
2. Interfaces at consumer, 1–3 methods. Prefer `io.Reader`/`io.Writer` over custom.
3. `ctx context.Context` first param on I/O/cancellable paths. Never store in struct (`containedctx`, `contextcheck`, `noctx` enforce).
4. `log/slog` only. No `fmt.Print*`, no bare `log.Printf`.
5. DI via constructors. No `init()` deps, no package-level service vars. No goroutines in `init()` — expose `Start`/`Stop`.
6. Naming: short receivers (`s`, `h`), acronyms `userID`/`httpClient`, no stutter, document exports.
7. No `any` in business logic. Generics or concrete types; `any` only at JSON/plugin boundary.
8. Resources: `defer Close()` immediately. HTTP bodies closed on callers (`bodyclose`).
9. Modernize: `slices.Contains`, `min`/`max`, `range-int`, `errors.AsType` (Go 1.26+). Run `go fix` modernizers before hand-rolling (`atomictypes`, `embedlit`, `slicesbackward`, `unsafefuncs`).
10. `new(expr)` (Go 1.26) only for optional pointer fields (e.g. `*int` JSON). Sparingly.

## Concurrency (deltas)

Full table: `references/go-concurrency-deltas.md`.

- Owner + exit + wait for every `go`. Use `wg.Go` (Go 1.25), never `Add` inside the goroutine.
- **Constructors build, callers start.** `New` never spawns; expose `Start(ctx)` and bind `cancel` to `se.App.OnTerminate` (see `router/credits.go`). A worker started in `New` on `Background` can never be stopped.
- Work that outlives the request (durable workflow poll) must NOT use `c.Request.Context()`. Derive from `Background` + a `done chan struct{}` (`synctest`-friendly, and `containedctx` forbids storing `ctx` in a struct). See `features/todo/handlers/onboarding.go`.
- Fan-out with errors: `errgroup.WithContext` + `SetLimit(n)` instead of hand pools.
- Channel direction at boundaries (`chan<-`, `<-chan`). Buffer 0 or 1; justify larger.
- Every long `select` has `<-ctx.Done()`. No `time.After` in hot loops (`NewTimer` + `Reset`).
- Mutex zero value, unexported `mu`, short sections, never across I/O. Counters/flags: typed `atomic.*`.
- Writes into a tree you don't fully control: `os.Root` (`os.OpenRoot`), not a lexical path check — a planted symlink defeats `filepath.Join` + `HasPrefix`. See `internal/installer/tree.go`.
- Tests: `goleak` for leaks (never `runtime.NumGoroutine()`), `synctest.Test`/`Wait`/`Sleep` for timers. Prod leaks: `goroutineleak` pprof (GA 1.27), not a test substitute.

## Performance

Full runbook: `references/go-perf.md`.

- pprof before SIMD before Zig. No evidence, no native code.
- `strconv` over `fmt` on hot paths. Pre-allocate slice/map. `strings.Builder` + `Grow`.
- Do not set `GOMAXPROCS` (container-aware default since 1.25). Do not add `automaxprocs` without measurement.
- `encoding/json` is v2-backed in 1.27 (faster decode, error text may differ). Do not migrate APIs manually.

## Testing

Full strategy: `references/go-testing.md`.

- Always `go test -race ./...` scoped; `make ci-local-fast` while iterating (changed packages only), full `make ci-local` (= CI) before push, `make signoff` stamps.
- **Test/request timeouts must exceed SQLite's `busy_timeout`** (10s here), or a request cancels while the DB is still legitimately waiting for the lock — the "intermittent `context deadline exceeded`" that is really lock contention.
- Test servers bind EPHEMERAL ports (`127.0.0.1:0`, NATS `-1`) and read the real address back from the server; a fixed port lets another package's test steal it under `-p N`, which reads as "needs `-p 1`" but is a collision.
- Table-driven for multi-case logic. `t.Helper()` in helpers (`thelper`).
- PB/SQLite: temp-dir instance + `Bootstrap()`, drive via `httptest`. LLM points: function-field injection, no VCR server.
- `B.Loop` style for benchmarks (1.26 inlining fix). `AllocsPerRun` panics under `-parallel`.

## Datastar (.templ)

Full rules: `references/datastar.md`.

- Gate: `make datastar-lint` = `bin/datastar-lint -only-errors -r ./features`. CI installs via `go install github.com/calionauta/datastar-lint@latest` (`.github/workflows/ci.yml`), local uses the wrapper.
- `PatchElements` top element needs `id` + `WithSelector` (else `PatchElementsNoTargetsFound`). Use `internal/datastar.RenderAndPatch`.
- Prefer Datastar attrs (`data-on:*`, signals, `__window`/`__document`) over vanilla JS. Intentional attrs go in `.datastar-lint.yaml` (`attributes.allowed`).
- `site/` and `docs/` are outside the Tailwind scan (`features/`, `web/`, `internal/` only). Landing edits cannot stale `app.min.css`.

## Zig Gate

Full gate: `references/zig-gate.md` (summary) + `docs/native-zig.md` (normative).

- Zero Zig in tree today. No vendored Zig skill until the first kernel passes the gate.
- Bans: "Zig is faster", manual memory, low-level, speculation, preference, avoiding a Go dep.
- When vendoring: exactly 1 pinned skill matching the toolchain (`zig-0.16` or `0.17`), never floating latest.

## Enforcement (authoritative)

| Check | Command |
|---|---|
| Format | `golangci-lint` gate (not bare `gofumpt`; versions differ) |
| Lint scoped | `golangci-lint run <changed-pkgs>` (27 linters, `.golangci.yml`) |
| Templ | `make templ && make datastar-lint` (when `.templ` changed) |
| Sizes/scope | pre-commit `file-sizes` + `go run ./cmd/check-scope` |
| Tests | `go test -race -p 1 <pkgs>`; full `make ci-local`; stamp `make signoff` |
| Vuln/deadcode | pre-push `govulncheck`, `deadcode -test` |
| Deps | `go mod tidy && git diff --exit-code go.mod go.sum`; audit adds with `go mod why` |

## Examples

### Example 1: Review concurrent code

**Input:** "Review this handler that spawns goroutines per item."

**Steps:**
1. Read `references/go-concurrency-deltas.md`.
2. Check every `go` has exit + wait (`wg.Go` or `errgroup`), `select` has `ctx.Done()`, channel dirs set.
3. Run scoped `go test -race` + `golangci-lint run <pkg>`.

**Output:** "2 fire-and-forget `go` without wait — converted to `errgroup.SetLimit(8)`, added `ctx.Done()` case, race clean."

### Example 2: .templ change

**Input:** "Added a Datastar morph fragment."

**Steps:**
1. Run `make templ && make datastar-lint`.
2. Verify top element has `id` + `WithSelector` via `RenderAndPatch`.
3. Run `make css` if classes changed, so `css-check` stays green.

**Output:** "datastar-lint clean, CSS rebuilt, `ci-local` gate unaffected."

### Example 3: Perf question

**Input:** "Should this parser go to Zig?"

**Steps:**
1. Follow `references/go-perf.md` then `references/zig-gate.md`.
2. Demand Go baseline + 30s CPU pprof + `GOEXPERIMENT=simd` evaluation first.

**Output:** "No Zig — pprof shows 80% in SQLite query, not CPU kernel. Fix query first."

## Edge Cases

- `_templ.go` files: excluded from `funlen`/`gocyclo`/`dupl`/`lll` (generated).
- `_test.go`: excluded from `goconst`/`mnd`/`funlen`/`gocyclo`/`bodyclose` (do not close httptest bodies).
- Morpheus `data-neo-*` attrs: intentional, keep `-only-errors`; add truly shared ones to `.datastar-lint.yaml`.
- `site/**`, `docs/**`: not scanned by Tailwind, not deployed in binary. Never gate them with `css-check`.
- `cmd/desktop`, `cmd/gui`: separate targets, excluded from web gate (`scripts/web-packages.sh`). Full lint manual (`make lint-gui`).
- Go 1.27 `gofmt` alignment churn: expect a one-time whitespace diff on old files.

## Test Cases

### Should activate
- "Fix this goroutine leak in the SSE hub"
- "Add context to this repository method"
- "Why does datastar-lint fail on this fragment?"
- "Write a benchmark for this codec"
- "Is this hot path a Zig candidate?"

### Should NOT activate
- "Write landing-page copy" (content, not Go)
- "Design the pricing page CSS" (skin concern, see `docs/ui-skins.md`)
- "Create a Zig kernel because it might be faster" (speculation — gate refuses without profile)
- "Bump the marketing site Tailwind" (`site/` has its own stylesheet)

## References

| File | What |
|---|---|
| `references/go-concurrency-deltas.md` | wg.Go, errgroup, channels, timers, goleak/synctest/goroutineleak |
| `references/go-perf.md` | pprof runbook, alloc, GOMAXPROCS, jsonv2, simd gate |
| `references/go-testing.md` | race, synctest, httptest, B.Loop, PB strategy, fix modernizers |
| `references/datastar.md` | wrapper, scope, PatchElements, whitelist, CSS scan roots |
| `references/zig-gate.md` | bans, decision, pin policy, what to vendor and when |
