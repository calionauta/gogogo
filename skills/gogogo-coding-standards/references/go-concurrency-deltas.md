# Go concurrency deltas (1.25–1.27)

Applies on top of SKILL.md core rules. Standard: `contextcheck`, `containedctx`, `noctx`, `govet` (`waitgroupgo` in 1.27).

## WaitGroup: use `wg.Go` (Go 1.25+)

```go
var wg sync.WaitGroup
for _, it := range items {
    wg.Go(func() { process(ctx, it) })
}
wg.Wait()
```

- `Go` folds `Add(1)` + `go` + `Done`. `f` must not panic.
- Old `Add`/`Done` still valid for tracking without spawning. Never `Add` inside the goroutine (`Wait` may return first — `go vet` flags it).

## errgroup: errors + cancel + bound

```go
g, ctx := errgroup.WithContext(ctx)
g.SetLimit(8)
for _, u := range urls {
    g.Go(func() error { return fetch(ctx, u) })
}
if err := g.Wait(); err != nil {
    return fmt.Errorf("fetching urls: %w", err)
}
```

`Wait` returns the first error and cancels `ctx`. Replaces hand-rolled worker pools.

## Channels: direction + size

```go
func produce(out chan<- int) { /* send-only */ }
func consume(in <-chan int) { /* receive-only */ }
```

- Only the sender closes. Receivers use `for v := range ch` or `v, ok := <-ch`.
- Buffer 0 or 1. Larger needs a comment (bound under load, writer-block behavior).
- Never send mutable pointers across; send copies or immutable values.
- Concurrent map read+write crashes: `sync.Map` or `RWMutex` + map.

## Select: always cancellable, no timer leak

Every long-running `select` includes `<-ctx.Done()`. No `time.After` in hot loops (allocates per iteration):

```go
t := time.NewTimer(d)
defer t.Stop()
select {
case v := <-ch:
    _ = v
case <-ctx.Done():
    return ctx.Err()
case <-t.C:
}
```

In a `for {}` loop this matters more: `time.After` allocates a new Timer every
idle tick. Keep one timer and `Reset` it — `internal/queue/workers.go` had this
bug (4 workers x ~1/s, one allocation each, forever):

```go
idle := time.NewTimer(d)
defer idle.Stop()
for {
    // ... work ...
    if idleAgain {
        idle.Reset(d)
        select {
        case <-ctx.Done():
            return
        case <-idle.C:
        }
    }
}
```

Before `Reset` on a timer whose channel may still hold a value, drain it:
`if !t.Stop() { select { case <-t.C: default: } }`.

This one is **linter-enforced**: `rules/rules.go` (ruleguard, loaded by
`gocritic`) flags `<-time.After(...)` as a select case, so the next occurrence
fails CI instead of shipping. A hoisted one-shot `timeout := time.After(d)` is
intentionally not flagged.

## Mutex vs atomic

- `sync.Mutex`/`RWMutex` zero value is valid; keep as unexported `mu`, never embed. Short sections, never across I/O.
- Counters/flags: typed `atomic.Uint64`/`atomic.Bool`. `sync.Once`/`OnceFunc`/`OnceValue` for one-shot init. `x/sync/singleflight` for stampede dedup.

## Leaks: test vs prod (do not confuse)

| Layer | Tool | When |
|---|---|---|
| Tests | `go.uber.org/goleak` (`VerifyTestMain` or `VerifyNone`) | every package that spawns goroutines |
| Tests | `testing/synctest` (`Test`, `Wait`, `Sleep`; `Run` removed in 1.26) | timer-dependent concurrency; fake clock, deterministic |
| Prod | `goroutineleak` pprof (exp 1.26 `GOEXPERIMENT=goroutineleakprofile`, **GA 1.27**: `runtime/pprof` + `/debug/pprof/goroutineleak`) | diagnose deployed leaks via reachability; misses globals/runnable-reachable blocks |

`go test -race ./...` always. No goroutines in `init()`.

## Lifecycle: constructors build, callers start (and stop)

A constructor must not spawn background work. If `New` starts a worker bound to
`context.Background()`, no caller can ever stop it — that is a leak by design.
Split construction from startup and let the OWNER bind the lifetime:

```go
// constructor: state only, no goroutines
func New(cfg *Config) (*Service, error) { ... }

// starter: the caller passes a ctx it can cancel
func (s *Service) Start(ctx context.Context) {
    s.started.Do(func() {           // idempotent — wiring may run twice in tests
        go s.loop(ctx)
    })
}

func (s *Service) loop(ctx context.Context) {
    t := time.NewTicker(interval)
    defer t.Stop()
    for {
        select {
        case <-ctx.Done():          // every long-lived loop has an exit
            return
        case <-t.C:
            drain(ctx)              // pass ctx, never context.Background()
        }
    }
}
```

Wire the cancel at the composition root, exactly like the repo does
(the credits Register in `features/credits/handlers/register.go` and
`AttachJetStream`):

```go
ctx, cancel := context.WithCancel(context.Background())
se.App.OnTerminate().BindFunc(func(e *core.TerminateEvent) error {
    cancel()
    return e.Next()
})
svc.Start(ctx)
```

**Durable work survives the client — so do not use `c.Request.Context()`.**
A workflow poll that must keep running after the browser navigates away
derives from `context.Background()`, then ties itself to app shutdown with a
`done chan struct{}` (closed once via `sync.Once`) rather than a stored
`context.Context` (which `containedctx` rejects anyway). See
`features/onboarding/onboarding.go` (`shutdownCtx`).

### Store the handle on its owner, or its lifecycle is unreachable

`StartWorkers()` returned a pool and the caller did
`workersLocal := q.StartWorkers(); _ = workersLocal`. The pool ran, nothing
could stop it, and `Close()` closed the database out from under it. Separating
construction from startup is only half the pattern — the OTHER half is that the
owner must hold what it started:

```go
// before — the lifecycle is discarded the moment it is created
ow := StartWorkers()
_ = ow

// after — the owner can always stop what it started
q.workers = wp          // set inside Start, so a caller cannot forget
func (q *Queue) Close() { if q.workers != nil { q.workers.Stop() }; ... }
```

Prefer storing it inside the starter (as above) over returning it for the
caller to keep: a returned handle that nobody assigns is the bug, and a return
value cannot enforce anything. Rule of thumb: **if a function starts something
long-lived, it is responsible for making it stoppable.**

### Only one component owns the process signal

`shutdownDagNats` was a no-op that nilled a pointer, with a comment asserting
the engine's shutdown "is wired internally". It was not, and the failure mode is
worth knowing because nothing about it looks wrong in the code: **a library may
register its own `signal.Notify` for SIGINT/SIGTERM** (DagNats does, in
`server.waitAndShutdown`; any second `signal.Notify` owner races the same way). The Go
runtime delivers a signal to EVERY registered channel, so both handlers run
concurrently and whichever finishes teardown first ends the process — skipping
the other's deferred cleanup entirely. Observed as: the log ends mid-shutdown
and `queue workers stopped` (the repo's own cleanup) never prints.

Two rules:

1. **Before trusting an embedded library's shutdown, check whether it calls
   `signal.Notify`** (and so does your host). If two components can see the same
   signal, drive shutdown yourself from the one you control — call the library's
   explicit `Stop()`/`Shutdown()` so its `Run()` drains through its normal path
   instead of racing a signal handler. Never write "shutdown is handled
   internally" without having read the code that does it.
2. **Verify shutdown by its effects, not by an exit code.** `go test`, `-race`
   and a clean `SIGTERM` exit all passed while the cleanups were being skipped.
   The assertion that caught it was a log line printed by the code that was
   supposed to run.

### Confine writes to a tree with `os.Root` (Go 1.24+)

A lexical guard (`filepath.Join` + `strings.HasPrefix`) blocks `../` but NOT a
symlink planted INSIDE the tree: with `root/evil -> /etc`, `root/evil/x` passes
the check and the write escapes. `os.Root` resolves each path against the
directory handle in the kernel and refuses a symlink that leaves the root:

```go
r, err := os.OpenRoot(dir)   // once
if err != nil { return err }
defer r.Close()
r.WriteFile("sub/file.go", data, 0o644)  // names are root-relative
r.MkdirAll("sub", 0o755)
r.Remove("old.go")
```

Use it whenever you write into a path derived from untrusted or user-supplied
tree content (installer/CLI, unpacking). Keep the lexical check too, as
defense in depth. See `internal/installer/tree.go`.

## Lost updates: the bugs `-race` cannot see

`-race` proves no two goroutines touch the same memory unsafely. It says nothing
about two goroutines each making a correct step whose INTERLEAVING loses data.
Channels are race-free and still lose events; a lease can be extended race-free
and still be expired before the job finishes. Four of these shipped here and
were found by modelling the design, not by running it.

**The shape to look for.** An operation that (a) reads shared state, (b) releases
the lock, then (c) acts on what it read:

```go
h.mu.RLock(); ch := h.clients[id]; h.mu.RUnlock()
h.mu.Lock();  delete(h.buffer, id); ...   // another goroutine, elsewhere
select { case ch <- data: }               // (c) after (b): goes nowhere
```

Rule: **a decision and the action it licenses are one lock hold.** Never
`Unlock` between "I looked up where this goes" and "I put it there". If the
action needs a write (append to a buffer), take the write lock — do not "reuse"
the read lock you already released.

**Three failure modes, three different guards.**

| Failure | `-race` | `go vet`/linters | What actually catches it |
|---|---|---|---|
| Lost update between correct halves (above) | silent | silent | a lock-shape assertion, e.g. park the delivery and `TryLock` to assert the hold |
| Error discarded in its own nil-check (`if err != nil { _ = err }`) | silent | `errcheck` with `check-blank: true` does NOT flag it | ruleguard rule `SwallowedErrorInCheck` (`rules/rules.go`) |
| Semantic race across two clocks/leases (a reservation expires while its request runs) | silent | silent | model it — see below |

**When a rule is not reachable by inspection, model it.** A temporal property
("this call cannot outlive its lease", "no event is lost across a reconnect") is
not visible in any single function, so no linter can state it. For those, write a
small TLA+ model and check it with everystate (Rust, no Java): `npx everystate
Spec.tla`. It is a DELIBERATE tool for designing a protocol (lease, queue,
reconnect, fan-out), not a CI gate — a spec encodes assumptions about the
implementation and silently lies once those change. The durable output of a
modelling pass is the failing test it motivates, not the spec.
