# Go testing (template strategy)

Enforced by: `thelper`, `testifylint`, `sloglint` + `make ci-local` (race, `-p 1` for DagNats engine stability).

## Tiers

- T3 scoped: `go test -race -count=1 <changed-pkg>`.
- T3.5 fast gate: `make ci-local-fast` — cheap checks + race tests for the changed packages only (~2-30s). Use while iterating.
- T4 gate: `make ci-local` (templ + datastar-lint + css-check + check-scope + lint + `go test -race -p 1` + build + smoke). T5: `make signoff` stamps.

## SQLite timeouts: the flake that looks like noise

The integration tests hit a real SQLite file, and SQLite serializes writers. Its
`busy_timeout` (10s in this repo's DSN — `db/pocketbase.go`, `internal/queue/goqite.go`)
is how long a writer BLOCKS waiting for the lock before giving up. A test/request
timeout SHORTER than `busy_timeout` cancels the request while the database is
still legitimately waiting, producing an intermittent
`context deadline exceeded` that looks like flakiness and is not:

- `requestTimeout` (test HTTP client) **must exceed** `busy_timeout` + handler
  overhead. See `features/todo/crud_test.go` (20s vs 10s).
- Every SQLite DSN needs `busy_timeout` + `journal_mode(WAL)` **in the DSN**
  (a `PRAGMA` statement does not cover pooled connections). `internal/queue/goqite.go`
  once opened without it, so concurrent queue writes failed with `SQLITE_BUSY`
  instead of waiting.
- Prove it mechanically, don't guess: hold a write transaction, then run a
  second connection with the short timeout → `database is locked`; with the long
  one → success. That is the causal proof.

## Concurrency tests

- `goleak.VerifyTestMain(m)` or `defer goleak.VerifyNone(t)` in packages that spawn goroutines. **Never assert on `runtime.NumGoroutine()`**: it is process-global and races sibling tests (observed counts going DOWN mid-test). Add `goleak.IgnoreCurrent()` and, for a test DB, `goleak.IgnoreTopFunction("database/sql.(*DB).connectionOpener")`. Used in `features/credits/lifecycle_test.go`, `internal/queue/ssehub_test.go`.
- `synctest.Test(t, func(t *testing.T) {...})` + `synctest.Wait()` for timer/async code (fake clock). `Run` is gone (1.26); use `Test`. `synctest.Sleep(d)` = sleep + settle. `httptest.NewTestServer` (1.27) gives an in-memory fake network that works with synctest — prefer it over real ports. See `features/credits/settlement_synctest_test.go`.
- `AllocsPerRun` panics under parallel tests by design; isolate it.

## Benchmarks

Use `B.Loop` style (1.26 inlining fix makes `B.N → B.Loop` conversion safe):

```go
func BenchmarkCodec(b *testing.B) {
    for b.Loop() {
        encode(payload)
    }
}
```

Keep a Go baseline bench before any SIMD/Zig claim. Emit `t.Attr("key","value")` / `ArtifactDir` where CI needs structured output.

## Test flakiness: it is almost always resource contention

Before re-running a red test, suspect contention, not randomness:

- **A timeout shorter than a blocking dependency's own timeout.** SQLite's
  `busy_timeout` (10s here) is how long a writer WAITS for the lock; a request
  or test timeout below it cancels while the DB is still legitimately waiting.
  Keep test/request timeouts above `busy_timeout` + handler overhead.
- **Orphan processes holding ports.** A previous `go run`/`make ci-local`
  binary still bound to `:18099`/`:8099` makes the next run fail
  nondeterministically. Check `pgrep -fl "web|gogogo"` and `lsof -ti :8099`
  before blaming the code; kill with `pkill -x web`.
- **`make ci-local` runs the browser smoke** after the build; if the build's
  temp binary was cleaned between runs, the smoke fails with `ENOENT`. Run
  `make smoke` or the gate itself, not the node script against a stale path.
- Under a loaded machine (parallel builds, other suites) the whole `-race -p 1`
  sweep is slower and these windows widen. That is why the fixes above target
  the timeouts rather than retries.

## Template seams (no VCR, no mock server)

1. Pure functions: unit-test directly.
2. Function-field injection: `streamFn`/`superviseFn` nil in prod, stubbed in tests.
3. Temp-dir PocketBase + `Bootstrap()` + real SQLite; drive handlers with `httptest.NewRecorder` (+ `datastar.NewSSE` for SSE). Do not close httptest response bodies in tests (`bodyclose` excluded for `_test.go`).

## Modernizers first

Before hand-editing idioms, try the push-button path:

```bash
go fix ./pkg/...          # 1.26+ revamp: dozens of fixers on the vet framework
```

1.27 adds `atomictypes`, `embedlit`, `slicesbackward`, `unsafefuncs`. `go test` now runs the `stdversion` vet check by default (too-new stdlib symbols flagged per `go.mod` + tags). `errors.AsType` (generic `As`), `reflect.Type.Fields/Methods` iterators, `slog.NewMultiHandler` are available — prefer them over hand code.

Hand-mutation red-proof for critical invariants: invert the guarded behavior, confirm the test fails, revert. Never commit the mutation.
