# Go testing (template strategy)

Enforced by: `thelper`, `testifylint`, `sloglint` + `make ci-local` (race, parallel across packages).

> **No `-p 1` in the default gate.** `make test` runs `scripts/test-web.sh`, a plain parallel sweep. `-p 1` survives only in `make coverage`, for a different reason (one `coverage.out` needs a single invocation). If you see `-p 1` described as "DagNats engine stability" anywhere, it is stale — see the note below.

## Tiers

- T3 scoped: `go test -race -count=1 <changed-pkg>`.
- T3.5 fast gate: `make ci-local-fast` — cheap checks + race tests for the changed packages only (~2-30s). Use while iterating.
- T4 gate: `make ci-local` (templ + datastar-lint + css-check + check-scope + lint + race tests + build + smoke). T5: `make signoff` stamps.

The suite runs in parallel across packages (`scripts/test-web.sh`). It used to
force `-p 1` "for DagNats engine stability" — the real cause was that engine
tests bound FIXED ports and two packages grabbed the same 18099 (`address
already in use`). They now bind ephemeral ports (`127.0.0.1:0`) and read the
address back from `srv.HTTPAddr()`: ~1m50 vs ~4m15, no serialization needed.
NATS ports stay fixed (the test must name them to connect) but are distinct.
When constructing coverage across packages, keep `-p 1` (a single
`coverage.out` needs one invocation).

### Harness bugs that look like slow tests

Four defects in this template's own test harness each read as "the suite is
slow" and each cost real wall-clock. Check these before optimizing anything:

- **A parked `Body.Read` ignores a wall-clock deadline.**
  `for time.Now().Before(deadline) { stream.Body.Read(buf) }` only re-checks the
  deadline between reads, and `Read` blocks until the next event — so a silent
  SSE stream waits one full heartbeat (15s), not the window requested. A "6s"
  negative assertion took 15.5s. Read in a goroutine and `select` on a timer,
  closing the body on expiry to unblock the reader (`pumpSSEUntil`).
- **An SSE transcript is not JSON.** It is `event:`/`data:` lines, and each
  Datastar payload additionally carries a literal `signals ` prefix — so
  `json.Unmarshal(transcript)` and `json.Unmarshal(payload)` both always fail.
  A predicate built on either can never fire and silently burns its whole
  timeout. Strip both layers first.
- **A test asserting an ABSENCE cannot short-circuit**, so it must drain its
  full window. Name that intent (`pumpSSEFor` + one `sseAbsenceWindow`
  constant) instead of calling the normal wait — otherwise a deliberate absence
  check is indistinguishable from a slow hang, and its timeout reads as a
  performance bug.
- **Never write a shared package global in a fixture.**
  `auth.CookieSecure = false` was assigned by four fixtures. It is the zero
  value, so the write was a no-op — and a genuine data race the moment two
  tests in a package overlap. Set shared state once in `TestMain`, or not at all.

Each of these was found by asking "why is this slow", not by reading the code.

### An assertion that can never fail is worse than no assertion

Before optimizing a slow test, **prove the test is capable of failing.** A
negative/absence assertion whose predicate matches a string that never reaches
the wire is green forever — and it reads as coverage, so it survives review and
hides the regression it was written for.

```go
// blind at ANY window: the hub path translates the event away before it is sent
recordEvent := func(s string) bool {
	return strings.Contains(s, `"event":"created"`)   // never on the wire
}
```

`TestTodoRecordsNotBroadcastViaHub` had exactly this: `streamTodo` decodes
`{event,id}` and emits a signals patch plus a full-list `#todo-list`
replacement, so the raw event string is never sent. Re-introducing the removed
`hub.Broadcast(...)` left it green at both a 6s and a 500ms window. Assert the
**wire symptom** (`lastItemSource:"remote"`, or the `#todo-list` patch) and it
fails in 1.5s.

The cheap version of this check, whenever you write or touch a negative
assertion: **inject the thing the test forbids, and confirm it fails.** If you
cannot make it fail, it is not a test. Note this cuts the other way too — a
shortened window will be blamed first when the real defect is the predicate.

### Production pacing that tests sleep through

The largest cost in this suite was not a harness bug at all — it was
**demonstration pacing in production code that every test then waited out**. A
delay added so a human can watch the UI (`"visible pace"`, `"so the user can
SEE the retry"`) is invisible to a test, which only asserts the ORDER of events.
Four such delays cost ~20s of suite wall-clock for zero coverage:

| Delay | Purpose | Now |
|---|---|---|
| `retryDemoInitialDelay` 1500ms | stepper lights up one step at a time | `(*TodoHandler).SetRetryDemoDelay` |
| `simulatedResponseDelay` 1500ms | the "got suggestions" toast lands *after* the "retrying…" one | `llm.NewSimulatedWithDelay` |
| `Queue.RetryConfig` 2s→4s | producer-visible backoff | `Queue.SetRetry` |
| `sseAbsenceWindow` 6s | "no record event leaked onto the hub" | 500ms (the leak path is in-process and synchronous) |

**The rule:** a delay that exists for a human must be injectable — a
`Set…Delay`/`With…Delay` seam with the production default unchanged — and the
test fixture dials it down. Do not "fix" a slow test by shortening the
constant: that changes what the demo does. And do not let a test assert the gap,
then the seam is not a cheat but the only honest encoding of intent (assert
order/eventually).

Two corollaries the same investigation produced:

- **A fixed `time.Sleep` before an assertion is almost always a poll in
  disguise.** "Give subscriptions a beat to settle" (500ms) and "wait for
delivery" (1s) are event waits; the event arrives in milliseconds on an
  in-process broker. Poll for the condition with a deadline and the fast path
  costs one slice. Keep a sleep ONLY when the assertion is an ABSENCE.
- **Anything expensive and identical per test belongs in `TestMain`.**
  `TestCrossSessionCreatePropagates` ran `go build ./cmd/web` per call (~8.5s)
  for a binary shared by several tests.

### `t.Parallel()` is the biggest single lever — audit before adding it

Parallelizing `features/todo` (59 tests) took 42s → 17.7s: each test builds its
own PocketBase + SQLite + goqite fixture (~660ms, of which ~490ms is PB's own
`Bootstrap`), so the package's wall-clock was ~50 payings of a per-test fixed
cost. Before adding `t.Parallel()` to a package, verify all three:

- **no `t.Setenv`** anywhere in it (the testing package panics: "test using
  t.Setenv … can not use t.Parallel");
- **no shared package global** written per test;
- **no shared connection/server singleton.** This one is easy to miss:
  `internal/nats`' `StartEmbedded` assigns the package globals
  `NS/NC/JS = nil, nil, nil` and `Stop()` clears them, so two parallel tests
  race (the detector points at `embedded.go`). `features/store/crdtstore`
  shares one embedded NATS connection, and parallel tests tear it out from under
  each other ("add stream: nats: connection closed"). Both need a per-test
  handle before they can parallelize.

Do not add `t.Parallel()` to make a package look faster if it then fails: a new
red under `-race` is a finding about the production code, not about the tests.

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
- **Ports in tests must be ephemeral.** Bind `127.0.0.1:0` (and NATS `-1`) and read the real address back from the server's accessor. A fixed port lets a test in another package steal it under `-p N` — `address already in use` — which looks like "the engine needs `-p 1`" but is a collision. Engine tests did exactly this with 18099 and it cost the suite its parallelism for months. When a port genuinely must be fixed (a client has to name the NATS port), give each package a **distinct** one.
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
