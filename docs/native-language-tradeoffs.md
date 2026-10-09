# Native language tradeoffs (exception candidates)

> Companion to [exception-to-go](exception-to-go.md). Go is deliberately
> absent from the table below: Go is the baseline every kernel is measured
> against, not a candidate. This page exists so a proposal for "a different
> language for one specific kernel" — required-justification item 5 in
> [exception-to-go](exception-to-go.md#required-justification) — starts from
> evidence instead of rediscovering it under time pressure.

Scope is fixed: one small kernel behind a C ABI, caller-owned buffers, a
pure-Go fallback shipping from day one. Each language is judged on the same
five axes:

- **FFI friction** — what crosses the boundary, and what breaks there
- **build cost** — unified build, cross-compile, CI time, `scratch`-static
- **stability** — does code written today compile in six months
- **ecosystem** — can an agent and a new hire work with it next quarter
- **hidden runtime** — anything the Go side cannot see or control

## The table

| Language | Biggest risk | Why it hurts this kernel |
|---|---|---|
| Zig (named default) | Manual lifetime across the boundary | A missed `free` in a long-running process is silent OOM |
| Rust | Build cost, and safety stops at the FFI line | `target/` bloat + slow CI break the unified build; `unsafe` + panic rules reintroduce the exact bug class Rust was meant to remove |
| Nim | Hidden runtime + small bus factor | `NimMain` + per-thread GC init is invisible global state behind the boundary; the community is a fraction of Zig's and Nim 3 announces breaking changes |
| Odin | Pre-1.0 churn + thin ecosystem | Monthly releases carry BREAKING changes; no package manager by design; LLM and hiring knowledge near zero |
| Mojo | Vendor roadmap + churn history | One company sets the pace; the C-ABI↔Go path is untraveled; the language is GPU-first, still missing general-purpose pieces |

## Zig — the default, with an admitted cost

Biggest risk is manual memory: every `alloc` needs its paired `free`, and
the failure mode is a leak, not a compiler error. The counterweight is that
the failure is **detectable and deterministic**: `std.testing.allocator`
fails tests on leak, `DebugAllocator.deinit()` returns `.leak` with a stack
trace, and the [lifetime/leak gate](exception-to-go.md#lifetimeleak-gate-deterministic-llm-executable)
makes those checks mandatory, including a loop test that catches accumulation
a single call hides.

Zig stays the default for checkable properties, all verified in this repo's
context: `zig cc` cross-compiles without a second toolchain, the stdlib is
deep enough that a kernel needs no package ecosystem (`std.net`,
`std.crypto`, `std.compress`, threads, SIMD), the static-lib-to-Go path is
demonstrated, and explicit allocators are auditable in review. The admitted
cost is pre-1.0 churn (0.14→0.15 rewrote Reader/Writer, `ArrayList`
conventions, and removed `usingnamespace` and `async`/`await`), which is why
the toolchain must be pinned and every kernel carries a pure-Go fallback.

## Rust — the build is the risk, not the borrow checker

Inside Rust, the safety story holds. Across a C ABI into Go, almost none of
it transfers, and the price is paid in build complexity:

- **Panics must not cross.** Unwinding from Rust into another language is
  undefined behavior; every `extern "C"` entry point must `catch_unwind` or
  the crate must set `panic = "abort"` (Rustonomicon on unwinding; the same
  rule bit a real Go↔Rust integration in gravitational/teleport#35995).
- **All FFI is `unsafe` by definition** — the compiler cannot enforce its
  rules on non-Rust code (Chromium's Rust FFI docs state this as the first
  safety consideration). Memory must be allocated and freed on the same side
  (`Box::into_raw` without a later `Box::from_raw` leaks), and freeing
  C-allocated memory with Rust's allocator (or vice versa) is UB. A CMU study
  of FFI boundaries found 9 leaks across 10 real libraries, including
  widely used compression crates.
- **Leaks are safe but still OOM.** Rust explicitly excludes leaks from
  memory safety, so a long-running kernel gets the same silent-growth failure
  as Zig, with less of a leak-detection culture around it.
- **Debuggers stop at the cgo line.** Neither gdb nor delve follows a stack
  across the Go↔native boundary; bisection needs `bpftrace`-level tooling.
- **Build cost is structural.** A mid-size project's `target/` runs
  462 MB–1.3 GB in dev profile (measured on Hyperqueue with rustc nightly;
  debuginfo and incremental caches dominate), per-service trees reach several
  GB, and slow compilation plus target size are perennial top complaints in
  the Rust survey. Cargo becomes a second build system every contributor pays
  for, which is exactly what the unified `go build ./cmd/web` forbids.

None of this disqualifies Rust for a specific kernel — but a proposal must
rebut every line above, not just cite "memory safety".

## Nim — pleasant to write, wrong-shaped for the boundary

Nim's risk is what the Go side cannot see. Calling Nim from C (and therefore
from Go via cgo) requires initializing the runtime with `NimMain()` and, for
any GC-managed type, initializing the collector **per calling thread**
(`setupForeignThreadGc` + `nimGC_setStackBottom`) — on Go's M:N scheduler
that means initializing in every exported function or pinning threads. Shared
references need manual `GC_ref`/`GC_unref`. The memory mode (`--mm:arc`,
`orc`, `atomicArc`, `yrc`, `refc`, …) becomes part of the build contract,
and the Go-interop mode (`--mm:go`) is documented as having "seen little real
world use — use at your own risk".

Two further discounts: the community is roughly half of Zig's by stars with
slower growth and no presence in the Stack Overflow admired-languages list,
and the Nim 3 roadmap announces real breaking changes (reworked `async`,
macros replaced by compiler plugins, explicit nil references, removal of
multi-methods). Pinning handles the second; nothing handles the hidden
runtime, which directly violates the stateless-boundary rule. Nim is a fine
choice for stand-alone tooling — it is the wrong shape for an invisible
kernel behind a Go API.

## Odin — good language, premature dependency

Odin's trajectory is genuinely healthy: a decade of work, real shipped
products (JangaFX), and a 1.0 ("Odin 2027") targeting RC by Christmas 2026
with a full language specification and backward-compatibility commitment.
The risk is timing and thickness:

- Monthly `dev-YYYY-MM` releases still carry BREAKING changes (mid-2026
  rewrote `core:os` into v2). Until the 1.0 spec lands, a pin is a bet on a
  moving target.
- No package manager by design — manual vendoring, which suits games and
  handmade software but adds process this repo does not have.
- Memory is manual via `context.allocator`: the same leak class as Zig, with
  a thinner detection culture and far fewer eyes.
- Ecosystem depth (books, production case studies, hiring pool, LLM training
  presence) is an order of magnitude below Zig, itself small.

Revisit after the 1.0 specification ships and holds for a few quarters. Until
then, a proposal must explain why Odin beats Zig for this specific kernel
despite the above — "cleaner syntax" is not that explanation.

## Mojo — best trajectory, worst timing for this use

Mojo 1.0 (Aug 2026) is real progress: core language features declared
stable, stdlib APIs individually marked stable (a compiler-enforced contract,
stronger than a blog promise), and nearly every breaking change shipping
with a deprecated alias plus a compiler fix-it. But three facts dominate for
a Go-side kernel:

- The 1.0 itself carried "more breaking changes than usual" — the freeze was
  bought with one last big churn, and 1.1 continued removing deprecated APIs.
  A stability *record* does not exist yet, only a stability *policy*.
- The compiler was proprietary until the Qualcomm acquisition (open-sourced
  Apache 2.0, Aug 2026). Depending on a single vendor's roadmap for a
  load-bearing build step is the opposite of the boring-build rule.
- The language is GPU-first and general-purpose-second: GPU APIs just moved
  (`std.gpu` → `max.gpu`), and async, pattern matching, and unions are still
  on the roadmap. The C-ABI↔Go interop path is essentially untraveled —
  no demonstrated static-link story, no leak-detection culture for this
  shape, minimal LLM knowledge.

Revisit when 1.x has a year of additive-only history and someone has shipped
the Go interop path. For GPU kernels the calculus may already differ; for a
CPU kernel behind a Go API it does not — yet.

## How to use this page

A justification item 5 that proposes a non-Zig language must cite the
corresponding row above and rebut it point by point, with the same
benchmark-and-profile evidence the Zig path requires. "Ergonomics" or
"syntax" alone rebuts nothing.

Revisit this page yearly, or when any of these land: Odin 1.0 spec holding
for two quarters, Mojo 1.x with an additive-only year, Nim 3 stabilizing, or
a demonstrated Go interop path for any of them.

## Related

- [exception-to-go](exception-to-go.md) — normative: decision procedure, boundary rules, lifetime/leak gate, removal plan.
- [motivation](motivation.md) — the thesis behind Go-first.
- [local-ci](local-ci.md) — the tier ladder any native change must climb.
