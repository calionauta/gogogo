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
