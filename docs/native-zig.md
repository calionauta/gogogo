# Native boundary (Go-first, Zig as escape hatch)

> Go is the product language. Zig is an exceptional native implementation tool.

Gogogo is overwhelmingly a Go application — approximately 95–99%+ normal
product code. Zig exists only as an **escape hatch** for the rare case where
Go demonstrably cannot do the job. This page is the decision guide for
humans and LLM coding agents building applications **on top of** gogogo:
when to stay in Go, when a native component may be justified, and the rules
that component must follow.

There is currently **no Zig code and no Zig toolchain** in this repository,
and none should be added without a concrete use case that passes the
[decision procedure](#decision-procedure) below with benchmark evidence.

## Why Go-first

- **Single binary, minimal ops.** One `go build ./cmd/web` compiles
  everything — no build tags, runtime opt-outs via env vars. See
  [Architecture](architecture.md).
- **Predictable builds and deploys.** The `Dockerfile` ships a static binary
  on `scratch` (~30 MB). Anything requiring a C toolchain or a second
  compiler complicates cross-compilation, reproducibility, and distribution.
- **LLM-friendly code.** Go's minimal syntax, `gofumpt` uniformity, static
  typing, and goroutine/channel concurrency are equally readable by humans
  and agents. Every new language doubles the cognitive surface.
- **The stack already covers the hard parts.** Database, auth, realtime,
  jobs, workflows, CRDT sync, LLM client — see
  [Stack in layers](stack-layers.md) and [Seven async layers](async-layers.md).
  Most "we need native" instincts are really "we haven't checked what Go or
  gogogo already ships."

## What "native" means

`native` is **not** a fourth SCOPE layer next to core / plugin / feature
(see [Scope taxonomy](scope-taxonomy.md)). It is an **exceptional
implementation boundary**: a small piece of functionality, owned by a normal
Go package, whose inner kernel happens to be implemented in a systems
language (Zig) behind a small, stable ABI.

```
Go application (95-99%+ : HTTP, auth, SQLite, jobs, realtime, LLM, UI, ...)
      |
      | small, explicit native boundary (C ABI, ownership documented)
      v
  Zig kernel (small, stateless, replaceable, deletable)
```

The goal is `Go → stable boundary → Zig`, never `Go application → Zig
everywhere`. Zig types, allocators, ownership semantics, and build details
must not leak into the Go architecture.

## Native is not synonymous with Zig

Distinguish the **native boundary** (the concept) from **Zig** (the
preferred implementation language). If a native component is needed, Zig is
the first language to evaluate — but do not force everything through Zig:

```
Go
  │
  ▼
Zig boundary
  │
  ├── Zig implementation
  │
  └── mature C/native library
```

- Zig may be the whole kernel (a few functions using only the Zig standard
  library), or just a thin, reproducible integration layer around a mature
  C/native library.
- Do not reimplement mature native functionality in Zig merely because Zig
  can do it. If the C library is proven, wrap it; do not rewrite it.
- Likewise, do not introduce a Zig dependency (or a C library) when a tiny
  amount of Zig stdlib code is sufficient.

## Zig dependency policy (stdlib first)

Priority order for solving any problem:

```
1. Go standard library / existing Go solution
2. Go library
3. Go optimization / compiler / SIMD facilities
4. isolated Zig + Zig standard library
5. mature C/native library through an appropriate boundary
6. specific Zig package with a clear advantage
7. Zig framework, only in exceptional circumstances
```

The rule is: **Zig standard library first.** Do not build a Zig ecosystem
inside gogogo. No Zig frameworks because they are popular, no third-party
Zig packages merely for convenience. A Zig dependency (level 6–7) or a C
library (level 5) must justify each of: why Go is insufficient, why Zig
stdlib is insufficient, why the dependency beats a small local
implementation, maintenance burden, portability, build complexity,
supply-chain implications, LLM coding complexity, project maturity, and how
easily it can be removed.

## No Zig application framework

Do not turn gogogo into `Go → native abstraction framework → Zig
framework → Zig libraries → functionality`. The Zig component should be
boring: `Go application → small explicit boundary → Zig component → Zig
stdlib / one justified native dependency`. Clever generic FFI
infrastructure, native plugin systems, or framework scaffolding without a
real use case are out of scope — see
[Do not over-engineer](#do-not-over-engineer-the-preparation).

## Why Zig is named, and not left open

Naming Zig is a deliberate decision, not an oversight. The rule this page
enforces is **no native code without a Go baseline and a profile**; it is not
"no preference about which toolchain". Those are different things, and the
difference matters because of how an agent behaves when it reaches the end of
the decision procedure.

**The cost of choosing at the moment of the bottleneck is the real argument.**
The moment a profile proves a bottleneck is the worst moment to spend tokens
learning a toolchain: the agent is under pressure, and it pays the **discovery**
rate instead of the **reuse** rate. Preparation done calmly — this page, the
`zig-gate` reference, the boundary rules, the removal plan — is what lets the
urgent moment reuse instead of discover.

That is why the default is **named** rather than left open. This page, the
`zig-gate` reference in the coding-standards skill, the boundary rules and the
removal plan are all preparation, and preparation only pays off if the language is
already decided. An agnostic gate would demote them to "one option among several",
and an agent reaching the end of the decision procedure would re-derive the choice
under time pressure — the worst possible moment.

Zig is the first language to evaluate because of concrete, checkable properties,
all of them verifiable here today:

| Property | Why it matters here |
|---|---|
| `zig cc` cross-compiles | Acts as the cross-`cc` for a cgo build, so a native kernel does not add a second cross-toolchain. **Verify per target**: measured here it reaches linux/amd64, linux/arm64, windows/amd64, windows/arm64 but **not** darwin/amd64 or darwin/arm64 (the Go linker passes `-lresolv` and Zig's bundled macOS SDK has no such stub — verified with a cgo program that does not even import `net`) |
| stdlib-first depth | `std.net`, `std.crypto`, `std.compress`, threads, SIMD — a kernel needs no package ecosystem, which is what keeps it "one package, small ABI, removable in minutes" |
| Proven static cgo linking | The static-lib-to-Go path is demonstrated, not assumed |
| Pre-1.0, pin-first | A **cost**, not a feature: the toolchain must be pinned and upgrades are real work. Acceptable because a native kernel is expected to be small and short-lived |

**When Zig is the wrong answer, argue it — do not quietly pick something else.**
If a mature C library already solves the problem, wrap it (see [Native is not
synonymous with Zig](#native-is-not-synonymous-with-zig)) instead of rewriting it.
If a different language is genuinely better for one specific kernel, the proposal
must make that case in required-justification item 5. What this policy forbids is
an agent **silently** choosing a language — or silently writing native code at all
— with neither the baseline nor the argument.

## When Zig may be justified

Concrete categories only — each still requires evidence per the
[decision procedure](#decision-procedure):

- specialized CPU-intensive kernels where profiling proves Go is the
  bottleneck
- SIMD-heavy computation where Go's SIMD facilities are demonstrably
  insufficient (see [Go SIMD note](#go-simd-check-first))
- architecture-specific intrinsics with no Go equivalent
- parsers, compression/decompression kernels, or binary-protocol processors
  where profiling shows a meaningful bottleneck
- image/audio/video processing kernels
- specialized serialization/deserialization fast paths
- memory-intensive algorithms requiring unusually precise layout/allocation
  control
- native OS/platform integration unavailable in Go
- interop with an existing C/native library where Zig is the simplest,
  thinnest boundary
- embedded/native-systems work outside Go's reach
- functionality unavailable or impractical in the Go ecosystem

## What must stay in Go

- Normal application/product logic — always Go.
- HTTP/API, auth, database, realtime, jobs, workflows, LLM, UI integration —
  always Go (gogogo already ships these).
- Anything where Go, the standard library, compiler optimizations, or an
  established Go dependency solves it adequately — Go.
- Anything justified only by speculation — Go.

The following are **never sufficient justification by themselves**:

- "Zig is faster"
- "Zig has manual memory management"
- "Zig is more low-level"
- "this could be optimized"
- "this looks like a good use of SIMD"
- personal preference for Zig
- avoiding a normal Go dependency
- hypothetical future performance

Performance claims require profiling and/or benchmarks against a Go
baseline. No evidence, no Zig.

## Go SIMD: check first

Current Go versions include experimental SIMD APIs (`simd` and
`simd/archsimd`). "We need SIMD" is **not** automatically a reason to use
Zig. Before proposing native code, check whether Go's standard facilities,
compiler optimizations, SIMD APIs, or existing Go libraries solve the
problem. Document what you tried and the measured gap.

## Decision procedure

Answer in order. The first "Go" wins — stop there.

```
Is this normal application/product logic?
    yes -> Go

Is this HTTP/API/auth/database/realtime/job/LLM/UI integration?
    yes -> Go

Is it performance-sensitive with a demonstrated hot path?
    no -> Go

Can Go (stdlib + compiler + simd/archsimd) solve it adequately?
    yes -> Go

Can an established Go library solve it adequately?
    yes -> Go

Is there a demonstrated bottleneck (profile) or capability gap?
    no -> Go

Is a native implementation expected to provide a meaningful,
measurable benefit, or a capability Go cannot reasonably provide?
    no -> Go

    yes -> Evaluate Zig (write the justification below first)
```

### Required justification

A Zig proposal must be a short note covering all of these, or it is
rejected:

1. **problem** — what the kernel computes, with inputs/outputs
2. **Go baseline** — what was prototyped in Go (or which Go library was
   tried) and why it falls short
3. **evidence** — profiles and benchmarks: workload, machine, numbers,
   before/after expectation
4. **expected benefit** — measurable (e.g. latency/throughput/memory), not
   adjectives
5. **why Zig is appropriate** — and why Zig stdlib is sufficient, or (if a
   dependency is proposed) why it is needed per the
   [dependency policy](#zig-dependency-policy-stdlib-first)
6. **boundary design** — the C-ABI function set, data layout, who
   allocates/frees (see below)
7. **portability** —Tiered targets (`GOOS`/`GOARCH`), cross-compile story,
   `scratch`-static story
8. **build/CI implications** — effect on `make dev`, `make build`,
   `make ci-local`, CI duration, developer setup (see below)
9. **maintenance cost** — who owns the Zig, toolchain pin, upgrade burden
10. **fallback strategy** — the pure-Go implementation that ships when Zig is
    disabled or deleted
11. **removal plan** — exact files to delete and the Go fallback that takes
    over, provable with `go build ./cmd/web`

## Boundary and ownership rules

If Zig is ever introduced, the component must be:

- **small** — one kernel, one job, a handful of exported functions
- **isolated** — owned by exactly one Go package; no transitive dependency
  from unrelated features
- **independently testable and benchmarkable**
- **stateless where practical** — no hidden globals; state lives in an
  explicit handle or in caller-owned buffers
- **replaceable and deletable** — a pure-Go fallback exists; deleting the
  native kernel leaves a working (possibly slower) binary
- **explicit about ownership and lifetime** — every buffer crossing the
  boundary documents: who allocates, who frees, with what function, on
  which side; no Go pointers retained by Zig after return; no Zig
  allocator state leaking into Go
- **C-ABI based** — plain `extern "C"` functions, explicit integer widths,
  no Zig-specific types in the Go signature
- **free of global state** crossing the boundary

Do not spread Zig concepts (allocators, `comptime`, error unions, slices-as-
fat-pointers) through Go code. The Go side sees `package thing` with a
`// SCOPE:` annotation like any other package; the Zig sources are an opaque
implementation detail behind it.

## Directory and package conventions

There is **no `native/` directory today, and none should be created
speculatively**. If a justified case ever arrives, follow the existing
conventions instead of inventing a parallel tree:

- Go API lives where the codebase already puts that kind of code:
  `internal/<thing>/` (infra) or `features/<thing>/` (product), with the
  normal `// SCOPE:layer=…,removal=…` annotation — the linter
  (`cmd/check-scope`, run via `make check-scope`) only walks those two
  trees.
- Zig sources, if any, live **inside that package** (e.g.
  `internal/<thing>/zig/`), never in a top-level `native/zig/...` forest
  of speculative kernels. A top-level `native/` would also collide with the
  existing meaning of "native" in this repo (`cmd/gui` native window,
  `cmd/desktop` native shell).
- `native` is not a capability: do not register it in
  `internal/capabilities`, do not offer it in the installer (`cmd/gogogo`),
  and do not gate it behind a fourth SCOPE value. The Go package keeps its
  normal SCOPE; the Zig kernel inherits the package's removal story.
- One justified kernel = one package. Never build a shared "Zig utils"
  layer that unrelated features import — that is how an escape hatch
  becomes a second stack.

## Feature with an optional native implementation

Do not make `native` a top-level feature category. A feature stays a normal
Go feature; only its hot kernel may go native:

```
feature
   │
   ├── normal Go code (orchestration, HTTP, UI, 99% of the feature)
   │
   └── tiny Zig kernel (only the genuinely native portion)
```

Go remains the default even inside performance-sensitive code: prefer
**small native kernel + Go orchestration** over **large Zig subsystem + Go
wrapper**. Never rewrite an entire Go subsystem in Zig because one function
is performance-critical.

## How Go and Zig communicate

- **Small C ABI.** A few `export fn` symbols with C-compatible signatures.
  Prefer batch calls over chatty per-item calls.
- **Caller-owned memory by default.** Go allocates input/output buffers,
  passes pointer + length, Zig writes results, Go frees. If Zig must
  allocate, it must also export a matching `free` and the Go wrapper must
  `defer` it on every path.
- **No callbacks holding Go pointers** unless you can prove the pinning and
  lifetime story; prefer synchronous, copy-in/copy-out calls.
- **No panics/exceptions across the boundary.** Errors return as status
  codes or result structs the Go wrapper translates to Go `error`s.
- **Deterministic builds.** Pinned Zig version, vendored or hash-locked
  inputs, reproducible flags. Document the exact toolchain in the package
  doc comment.
- **C ABI is the default, not a dogma.** If another boundary is
  demonstrably simpler and safer for a particular use case, document the
  tradeoff rather than forcing C ABI mechanically — but the burden of proof
  is on the alternative.

## Performance rule (no reputation-based optimization)

"Zig is faster than Go" is not evidence. When performance motivates native
code:

1. establish a representative workload
2. implement a reasonable Go baseline
3. profile it
4. benchmark it
5. identify the actual bottleneck
6. determine whether Go's existing facilities (stdlib, compiler, SIMD) can
   address it
7. prototype Zig only if justified
8. benchmark Go vs Zig under the same workload
9. keep Zig only if the benefit justifies the added complexity

Judge beyond raw throughput: latency, memory usage, binary size, startup
cost, build time, cross-compilation, operational complexity, maintenance,
debugging, and agent/LLM coding complexity. A faster component that
significantly increases system complexity may be the worse engineering
decision.

## Reliability and LLM-friendliness

Gogogo is designed to be predictable for humans and LLM agents. Evaluate
every native proposal against: number of new concepts an agent must learn,
number of build systems involved, toolchain complexity, dependency count,
error-repair cycles, testability, determinism, documentation burden,
cross-platform behavior, and ease of local development. Prefer simple,
boring, explicit Zig over sophisticated abstractions — code an LLM can read
and modify safely.

## Testing and benchmarking requirements

- A **Go baseline benchmark** (` BenchmarkXxx`, realistic workload) exists
  **before** the Zig kernel lands, with numbers recorded in the
  justification.
- The native kernel gets **focused tests**: correctness against the Go
  baseline on fixed vectors (golden tests), edge cases (empty, max-size,
  malformed input), and allocation/lifetime tests (no leak on error paths).
- The native kernel gets a **benchmark** proving the claimed benefit on the
  same workload and machine class as the baseline.
- **Hand-mutation red-proof** (per `AGENTS.md` testing discipline): invert
  or remove the guarded behavior, confirm the test fails, then revert.
  Never commit a mutated line.
- Fuzz the boundary where inputs are untrusted (parsers, codecs, protocol
  handlers).

## Build, CI, and portability expectations

Adding a Zig toolchain conflicts with properties this repo protects.
Address each explicitly in the proposal; if any answer is "we can't do
this cleanly," stay in Go. Concretely, evaluate the impact on `make dev`,
`make build`, and `make ci-local`, plus:

- **Unified build.** `go build ./cmd/web` compiles everything with no tags.
  A native kernel must not force build tags, a second build step humans
  forget, or a binary that silently drops the feature when Zig is absent
  (a loud build error or a documented pure-Go fallback — never silent
  drift).
- **Cross-compilation.** The deploy workflow cross-compiles
  (`GOOS=linux` from any runner). The proposal must state which
  `GOOS`/`GOARCH` pairs the kernel supports and what happens on the rest
  (fallback vs build error).
- **Reproducibility.** Pinned toolchain, locked inputs, same bytes from the
  same source. No "install latest Zig" steps.
- **Static/`scratch` distribution.** The runtime image is `scratch` with a
  static binary. A kernel needing dynamic libraries, shell-outs, or OS
  services absent from `scratch` needs a distribution story, not a hope.
- **CI complexity.** Today CI installs Go + `gcc` (for `loro-go`) and runs
  one gate (`make ci-local`). A Zig step must be scoped (only when native
  sources change), cached, and pinned — or it taxes every push for a
  feature 99% of contributors never touch.
- **Developer setup.** `Go 1.27+` is currently the only prerequisite. If
  Zig becomes required for local builds, that is a project-level decision
  with docs, `Makefile`, and hook updates — never a drive-by.
- **Native build surface.** Use Zig's own build system where appropriate,
  but keep it isolated: one scoped entry point the `Makefile` calls only
  when native sources change, never a framework normal Go development
  depends on.
- **Debugging.** State how to get symbols/stack traces across the boundary
  and how to bisect a bug to the Go side vs the Zig side.

## Reliability and maintenance cost

| Concern | Question to answer | Bias |
|---|---|---|
| Cross-compilation | Which targets build? What falls back? | Stay in Go if the matrix grows |
| Build reproducibility | Pinned toolchain + locked inputs? | Stay in Go if "latest" is the pin |
| CI complexity | Scoped, cached, pinned step? | Stay in Go if every push pays |
| Developer setup | Still Go-only for most contributors? | Stay in Go if onboarding needs Zig |
| Binary distribution | Static/`scratch`-safe? | Stay in Go if dynamic linking leaks in |
| Platform support | All deploy targets covered? | Stay in Go if a tier-1 target drops |
| Debugging | Symbols + bisection story? | Stay in Go if failures become opaque |
| Testing | Baseline + kernel + red-proof? | Stay in Go if only vibes exist |
| Dependency management | Who upgrades Zig and when? | Stay in Go if nobody owns it |
| LLM coding complexity | Can an agent edit safely with repo skills? | Stay in Go if the boundary needs tribal knowledge |
| Upgrade burden | Re-evaluated on each Zig bump like other workarounds? | Stay in Go if bumps are unowned |

If Zig introduces disproportionate complexity for the feature's value,
the documented recommendation is to stay in Go.

## Removal

Every native kernel must be removable in minutes:

1. Delete the Zig sources (`internal/<thing>/zig/`).
2. Flip the Go wrapper to the pure-Go fallback (which shipped from day
   one, not written at removal time).
3. Remove any build/CI wiring the kernel added.
4. Prove with `go build ./cmd/web` and the package's tests.

The SCOPE doc comment on the owning Go file records this exact list, so a
future agent never has to guess. An agent must be able to remove a Zig
component without destabilizing the rest of the application — if removal
touches unrelated packages, the boundary was drawn wrong.

## Examples

Good candidate: profiling shows one binary-processing kernel consumes ~45%
of CPU time, and a small Zig implementation cuts that cost substantially
behind a few functions. → Evaluate Zig.

Good candidate: a required native OS capability has no practical Go
implementation, but Zig exposes a small C-compatible interface. → Evaluate
Zig.

Potentially good candidate: a mature C library is required and Zig provides
a clean, reproducible build/integration boundary around it. → Evaluate the
wrapper, not a rewrite.

Bad candidate: an ordinary CRUD/API feature could be written in either
language. → Go.

Bad candidate: a parser "might be faster" in Zig with no measured
bottleneck. → Go.

Bad candidate: Zig for manual memory management without a concrete
requirement. → Go.

Bad candidate: a Zig framework because it is popular, without an
architectural need. → Do not introduce it.

Bad candidate: SIMD is needed but Go's SIMD facilities are adequate. → Go.

## Do not over-engineer the preparation

Do NOT: add Zig to prove the architecture, add a Zig framework, build
speculative native abstractions or generic FFI infrastructure, design a
native plugin system for future possibilities, add example Zig packages
without real requirements, or make Go development depend on Zig. The desired
outcome of this page is mostly guidance — and a repository that still
contains **zero Zig code** is a successful outcome, not an incomplete one.

## Rules for coding agents

1. Go is the default implementation language. Prove otherwise with numbers.
2. Zig is exceptional. Most proposals should end in Go.
3. Never introduce Zig on performance speculation — profile first.
4. Check Go stdlib, compiler optimizations, SIMD APIs, existing gogogo
   capabilities, and established Go libraries before proposing native code.
5. Performance-driven Zig work requires benchmark/profile evidence against a
   Go baseline. Attach it to the proposal.
6. Prefer Zig stdlib over third-party Zig packages; avoid Zig frameworks;
   a mature C library may be wrapped, never rewritten without reason.
7. Keep native components isolated behind a small C ABI; one kernel, one
   owner, no shared "zig utils."
8. Document ownership and lifetime for every buffer crossing the boundary.
9. Ship focused tests + benchmarks + a red-proof for every kernel.
10. Never let Zig become a transitive dependency of unrelated features.
11. Never refactor normal Go code into Zig for architectural purity, and
    never add speculative native infrastructure.
12. Every kernel ships with a pure-Go fallback and a removal plan. Deletion
    must leave a working binary.

Vague instructions like "use Zig when appropriate" are banned. Follow the
[decision procedure](#decision-procedure) and write the
[justification](#required-justification) — or stay in Go.

## Related

- [Architecture](architecture.md) — unified build, directory layout, wiring.
- [Scope taxonomy](scope-taxonomy.md) — why `native` is a boundary, not a layer.
- [Stack in layers](stack-layers.md) — what Go and the dependencies already cover.
- [Code quality](code-quality.md) — lint expectations for any new package.
- [Local CI](local-ci.md) — the tier ladder any native change must climb.
