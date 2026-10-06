---
name: gogogo-coding-standards
description: "Go backend standards for the gogogo template — PocketBase/SQLite, Templ, Datastar, NATS JetStream, goqite, slog, goroutines/channels/mutex/errgroup, context propagation, golangci-lint, race/synctest/goleak tests, datastar-lint on .templ, pprof/simd/Zig gates. Triggers when: editing .go files, spawning goroutines, creating channels, wiring context, running lint/tests, touching .templ, profiling, or proposing native code. Delegates universal principles to stelow-workflow-coding-standards."
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
- **No bare `time.Sleep` in production code — it cannot be cancelled.** A sleep on a retry/backoff path blocks its goroutine through shutdown, and it is also unauditable latency in front of a user. `select` on `ctx.Done()` vs `time.After`/`time.NewTimer` instead; a `ticker` loop selects on `ctx.Done()`. This is not theoretical: `internal/nats`'s exported service globals were the same class of shortcut, and `time.Sleep(time.Second)` on the queue worker's error path is real code today (`internal/queue/workers.go`).
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
- **A delay that exists for a HUMAN must be injectable, and a test must assert an event, not a gap.** Demonstration pacing (`"visible pace"`, 1.5s retry gaps, 2s retry backoff) is invisible to a test; leaving it hard-coded cost this suite ~20s. Expose `Set…Delay`/`With…Delay`, keep the production default, dial it down in the fixture. A fixed `time.Sleep` before an assertion is usually a poll in disguise — poll with a deadline; keep the sleep only for an ABSENCE check.
- **Prove a negative assertion can fail before trusting it.** Inject the thing it forbids and confirm it goes red; a predicate matching a string the wire never carries is green forever and was already found once (`broadcast_probe_test.go`).
- `t.Parallel()` is the biggest single lever on wall-clock (per-test fixtures like PocketBase are the fixed cost). Audit first: no `t.Setenv`, no shared package global, no shared connection/server singleton — otherwise it is a red race, not a speed-up.
- **Test/request timeouts must exceed SQLite's `busy_timeout`** (10s here), or a request cancels while the DB is still legitimately waiting for the lock — the "intermittent `context deadline exceeded`" that is really lock contention.
- Test servers bind EPHEMERAL ports (`127.0.0.1:0`, NATS `-1`) and read the real address back from the server; a fixed port lets another package's test steal it under `-p N`, which reads as "needs `-p 1`" but is a collision.
- Table-driven for multi-case logic. `t.Helper()` in helpers (`thelper`).
- PB/SQLite: temp-dir instance + `Bootstrap()`, drive via `httptest`. LLM points: function-field injection, no VCR server.
- `B.Loop` style for benchmarks (1.26 inlining fix). `AllocsPerRun` panics under `-parallel`.

## Datastar (.templ)

Full rules: `references/datastar.md`.

- Gate: `make datastar-lint` = `bin/datastar-lint -only-errors -r ./features ./internal`. Two trees because the **Go analyzer** checks backend SDK calls (`sse.PatchElements`) under `internal/`; the HTML analyzer covers `.templ` under `features/`. CI pins the version (`@v0.12.0`), not `@latest` — a release can raise a rule to ERROR and fail the build with no code change. Local wrapper forwards `"$@"` and supplies `--analyzers html,go`.
- **The SDK is method-only.** `sse.PatchElements(html, datastar.WithSelector("#id"))` — there is no `datastar.PatchElements(sse, …)` in any 1.x release (it does not compile). `PatchElementf`/`RemoveElementf`/`RemoveElementByID` take no options, so they cannot lack a selector. A wrapper forwarding `opts ...PatchElementOption` is correct and must not be flagged.
- `PatchElements` top element needs `id` + `WithSelector` (else `PatchElementsNoTargetsFound`). Use `internal/datastar.RenderAndPatch`.
- Prefer Datastar attrs (`data-on:*`, signals, `__window`/`__document`) over vanilla JS. Intentional attrs go in `.datastar-lint.yaml` (`attributes.allowed`).
- `site/` and `docs/` are outside the Tailwind scan (`features/`, `web/`, `internal/` only). Landing edits cannot stale `app.min.css`.

## Zig Gate

Full gate: `references/zig-gate.md` (summary) + `docs/native-zig.md` (normative).

- Zero Zig in tree today. No vendored Zig skill until the first kernel passes the gate.
- Bans: "Zig is faster", manual memory, low-level, speculation, preference, avoiding a Go dep.
- When vendoring: exactly 1 pinned skill matching the toolchain (`zig-0.16` or `0.17`), never floating latest.
- **API truth is ZLS, never a skill.** No Zig-team skill exists; `zigcc/skills` is a community project. Confirm signatures with ZLS or the local std sources (`zig env`) before writing a call.

## Enforcement (authoritative)

| Check | Command |
|---|---|
| Format | `golangci-lint` gate (not bare `gofumpt`; versions differ) |
| Lint scoped | `golangci-lint run <changed-pkgs>` (31 linters, `.golangci.yml`) |
| Custom rules | `rules/rules.go` via `ruleguard` (`.golangci.yml` → `gocritic.settings.ruleguard`) — project footguns, CI-blocking |
| Templ | `make templ && make datastar-lint` (when `.templ` changed) |
| Sizes/scope | pre-commit `file-sizes` + `go run ./cmd/check-scope` |
| Tests | `go test -race <pkgs>`; full `make ci-local`; stamp `make signoff` |
| Vuln/deadcode | pre-push `govulncheck`, `deadcode -test` |
| Deps | `go mod tidy && git diff --exit-code go.mod go.sum`; audit adds with `go mod why` |

### Working with the linters (three layers, in the order they fire)

**1. `golangci-lint` — the 31-linter baseline.** Config is `.golangci.yml`; it is the
single source for which linters run. Do NOT run `golangci-lint run ./...` for a
small change — scope it (`golangci-lint run <changed-pkgs>`); the full repo is
~10x slower. Linters are grouped by role:

- **correctness** (`errcheck`, `govet` with `enable-all`, `staticcheck`, `ineffassign`, `nilerr`, `errorlint`)
- **concurrency/lifecycle** (`containedctx`, `contextcheck`, `noctx`, `gocritic`, `thelper`)
- **security** (`gosec`)
- **style/size** (`revive`, `dupl`, `funlen`, `gocyclo`, `lll`, `mnd`, `goconst`, `tagliatelle`, `modernize`, `perfsprint`, `usestdlibvars`)

Adding a linter: enable it under `linters.enable` in `.golangci.yml`, then run
`golangci-lint run <pkgs>` and fix what it reports **before** committing — a
newly enabled linter that fails CI is worse than not enabling it. A rule with
false positives here teaches people to add `//nolint`, which is the opposite of
the intent.

**2. `ruleguard` — project-specific footguns (`rules/rules.go`).** Wired through
`gocritic` (`settings.gocritic.settings.ruleguard.rules`), so it runs inside the
same `golangci-lint` pass and is CI-blocking. Use it for a footgun that no stock
linter covers — i.e. something this project got wrong at least once. Current
rules: `TimeAfterInSelect` (timer leak in a loop) and
`BlockingReadBehindDeadline` (a parked `Read` outliving its deadline).

Writing one — the DSL has three traps that all fail silently or confusingly:

- Variadic group is `$*name`, **not** `$$$name` (does not parse). It needs a
  name because `Where()` refers to it.
- A metavariable cannot be a selector receiver: `$x.Read($*_)` does not parse;
  `$x.Read($_)` does.
- Patterns match **expressions**, not statements — `for`/`select` bodies need the
  `$*body` form shown above, and `m.File().Text` does not exist.

A rule that fails to load surfaces in `golangci-lint` as the generic
`ruleguard: execution error: used Run() with an empty rule set`, which does not
name the real parse error. Debug it in a scratch module instead:

```bash
go run github.com/quasilyte/go-ruleguard/cmd/ruleguard@v0.4.5 -rules rules/rules.go .
```

`golangci-lint` caches results, so run `golangci-lint cache clean` before
believing a rule change had no effect.

**3. `datastar-lint` — the Datastar surface** (`.templ` attributes + Go SDK
calls). See `references/datastar.md`; the client-side rules `golangci-lint`
structurally cannot see. Note it only **blocks** on `ERROR`-severity findings —
warnings print and exit 0.

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
| `references/go-perf.md` | pprof runbook, alloc, GOMAXPROCS, jsonv2, simd gate, demonstration-delay latency |
| `references/go-testing.md` | race, synctest, httptest, B.Loop, PB strategy, fix modernizers |
| `references/datastar.md` | wrapper, scope, PatchElements, whitelist, CSS scan roots |
| `references/zig-gate.md` | bans, decision, pin policy, what to vendor and when |
