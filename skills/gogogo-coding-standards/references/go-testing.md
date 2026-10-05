# Go testing (template strategy)

Enforced by: `thelper`, `testifylint`, `sloglint` + `make ci-local` (race, `-p 1` for DagNats engine stability).

## Tiers

- T3 scoped: `go test -race -count=1 <changed-pkg>`.
- T4 gate: `make ci-local` (templ + datastar-lint + css-check + check-scope + lint + `go test -race -p 1` + build + smoke). T5: `make signoff` stamps.

## Concurrency tests

- `goleak.VerifyTestMain(m)` or `defer goleak.VerifyNone(t)` in packages that spawn goroutines.
- `synctest.Test(t, func(t *testing.T) {...})` + `synctest.Wait()` for timer/async code (fake clock). `Run` is gone (1.26); use `Test`. `synctest.Sleep(d)` = sleep + settle. `httptest.NewTestServer` (1.27) gives an in-memory fake network that works with synctest — prefer it over real ports.
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
