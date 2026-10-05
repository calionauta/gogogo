# Go perf runbook (pprof → simd → Zig)

Order is mandatory. Skipping to SIMD/Zig without a profile is rejected per `docs/native-zig.md`.

## 1. Ship correct first

`make signoff` green, `go test -race`, no N+1. Correctness before speed.

## 2. Measure prod-like

Bench + load: p99 latency, RSS, alloc churn. Guess nothing.

## 3. pprof (the X-ray)

```bash
import _ "net/http/pprof"  # then /debug/pprof/
go test -cpuprofile cpu.prof -memprofile mem.prof ./pkg
go tool pprof cpu.prof
```

Profiles: `cpu`, `heap`, `allocs`, `goroutine`, `goroutineleak` (1.27 GA), `block`, `mutex`, `trace` (+ `runtime/trace.FlightRecorder` for rare events). Capture ~30s CPU under load, read `top` before theorizing. 90% of wins are algorithm, query, alloc, buffer — not language.

## 4. Cheap Go wins first

- `strconv` over `fmt` on hot paths. Pre-allocate slice/map with cap. `strings.Builder` + `Grow`.
- `io.ReadAll` (1.26: ~2x, less garbage), `bytes.Buffer.Peek`, `strings`/`bytes.CutLast`.
- Small-alloc: 1.27 size-specialized malloc cuts <80B allocs up to 30% (~1% real programs, +60KB binary). No action — just know micro-alloc pressure dropped.
- `encoding/json` is v2-backed in 1.27 (decode much faster, `error` text may differ). Do not hand-migrate APIs; keep v1 calls. Opt-out `GOEXPERIMENT=nojsonv2` only with an issue filed.
- Green Tea GC: exp 1.25, default 1.26, SIMD-accelerated scan on new amd64. Expect 10–40% less GC CPU on GC-heavy workloads, ~1–4% total. No flag needed on 1.27.
- cgo baseline ~30% cheaper (1.26). Still avoid cgo on hot paths.

## 5. GOMAXPROCS: do not touch

Container-aware default since Go 1.25 (cgroup bandwidth, periodic update; disabled if `GOMAXPROCS` set or `GODEBUG=containermaxprocs=0,updatemaxprocs=0`). Do not set manually, do not add `automaxprocs` without a measured case. `runtime.SetDefaultGOMAXPROCS` exists for re-enabling the default after an override.

## 6. SIMD: portable first, experimental always

`GOEXPERIMENT=simd` required. 1.26: `simd/archsimd` amd64 only. 1.27: revised amd64 + arm64 NEON 128-bit + wasm 128-bit + portable `simd` (`Int8s`, `Float32s`, width-agnostic, emulated where no HW). Bridge via `ToArch()`/`FromArch()` for gaps. Test widths with `GODEBUG=simd=0/128/256/512`, `Emulated()`, `VectorBitSize()`. 1.28 plans SVE + reductions/shuffles (`ReduceSum`, `OnesCount` missing in 1.27 — use arch escape hatch with build tags).

Rule: try portable `simd` before any Zig. 2–7.5x happens only in vectorizable kernels (hash, codecs, ChaCha); CRUD HTTP gains ~zero (bound on alloc/scheduler/syscall/GC).

## 7. Then Zig (rare)

Only with a Go baseline bench + profile proving Go is the bottleneck and a measured expected benefit. See `references/zig-gate.md`. req/s parity with Rust/Zig is not the goal: I/O-bound paths already win in Go; GC imposes a pure-render ceiling.
