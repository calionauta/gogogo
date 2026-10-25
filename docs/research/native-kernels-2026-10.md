# Native kernels in practice: FNV-1a, grayscale, and the loader question (2026-10-25)

Reviewed: 2026-10-25

The question: do the native-exception rules in [exception-to-go](../exception-to-go.md)
hold up when actually measured — and what reusable evidence does a PoC
campaign produce? Two kernels, three to five legs each, same machines,
median-of-3. Verdict first: **the procedure worked exactly as designed.**
One kernel died at "Go solves it", the other survived to the SIMD rung and
narrowed to a documented verbs gap. No threshold or hierarchy changed;
the refinements below are additive.

All PoCs lived in `/tmp` (never versioned, since removed). Toolchains were
pinned and hash-verified. Host for every number: linux/arm64, shared box,
benches bounded (`-benchtime=200 ms`, `-count=3`).

## PoC-1: FNV-1a 64 — the common ending

Three legs, same workload: pure Go, pure Zig standalone, Zig via CGO.

| input | pure Go | pure Zig | Zig via CGO | allocs |
|---|---|---|---|---|
| 64 B | 97 ns (660 MB/s) | 98 ns (653 MB/s) | 158 ns (406 MB/s) | 0 everywhere |
| 4 KB | 7046 ns (581 MB/s) | 7150 ns (573 MB/s) | 7177 ns (571 MB/s) | 0 everywhere |
| 64 KB | 112252 ns (584 MB/s) | 114962 ns (570 MB/s) | 113420 ns (578 MB/s) | 0 everywhere |

Correctness: official FNV-1a goldens pass in both; Zig == Go on
0/1/63/64/65/4096/65536-byte inputs; nil == empty; `go test -race` green.
Red-proof: flipped prime → `zig test` FAIL and `go test` FAIL; revert →
green. Leak gate passes trivially: no allocation crosses the boundary.

Reading: kernels tie; the only real difference is the **~60 ns per-call
CGO cost**, visible at 64 B, noise at 4 KB+. Live proof of the batch-calls
rule. Verdict per the decision procedure: Go.

## PoC-2: RGB→grayscale — the rare survivor

Straight-line map kernel (`(77R+150G+29B)>>8`), boundary one level richer
(caller owns input AND output buffers, errors as statuses). Legs: pure Go
scalar, pure Zig standalone, Zig via CGO.

| image | pure Go | pure Zig | Zig via CGO |
|---|---|---|---|
| 64×64 | 12642 ns (972 MB/s) | 4766 ns (2578 MB/s) | 5549 ns (2215 MB/s) |
| 512×512 | 815201 ns (965 MB/s) | ~316000 ns (2489 MB/s) | ~370000 ns (~2125 MB/s) |
| 1024×1024 | 3.37 ms (931 MB/s) | 1.35 ms (2332 MB/s) | 1.49 ms (~2110 MB/s) |

Zig ≈ 2.2–2.6x scalar Go, stable across 12 KB–3 MB (LLVM autovectorizes
the map; Go's backend does not, on this shape).

The required next rung was audited, not skipped. Go 1.27's portable `simd`
covers lane-wise arithmetic, convert, and reshape only: **no widen,
no shuffle/deinterleave, no narrow**. `archsimd` arm64 has
`ExtendLo8ToUint16`, Mul/MulAdd, and interleave ops — but **no logical
shift-right for `Uint16x8` and no narrow to `Uint8`**. The kernel is
inexpressible in Go SIMD on arm64 short of assembly. That verbs audit
(~5 greps, zero lines written) is the evidence item 5 needs, and it is
why a survivor still owes the arch-specific attempt cost (or its
documented skip), the purego story, a fallback, and the human gate.

## Extra legs

**P1 — purego on the FNV kernel** (`zig build-lib -dynamic`, purego
v0.11.2): ~700 ns vs ~60 ns CGO per call at 64 B (plus 72 B/4 allocs per
call on that path; CGO had 0/0); parity at 64 KB. The preferred loader
costs ~10x per call where FFI is frequent — loader choice is load-bearing
exactly in the regime the batch rule governs.

**P2 — codegen matrix** (`zig build-lib`, one file): linux/windows/macos
pairs each green in <0.15 s. Codegen is not a linked binary: the
documented darwin `-lresolv` failure lives at Go-cgo-link time and was
not re-tested here. Scale note: a static lib is kilobytes (5.8 KB), a
linked standalone binary carrying its runtime is megabytes (~4 MB) —
quote both when arguing distribution.

**P4 — subprocess price tag**: spawn floor 812 us/op (`/bin/true` x200),
≈13,500x a CGO call. A worker process amortizes it; per-request exec
does not. This is the number the "C ABI is the default, not a dogma"
paragraph can cite.

**Rust probe** (row-targeted, not a shootout): cold build 0.2 s /
incremental 0.13 s on a trivial no-dep crate — understates scale pain by
construction; `target/` 24 MB, staticlib 24 MB default / 14 MB stripped
(std linked in) vs 5.8 KB Zig. Ceremony that bit live: the 2024 edition
moved `#[no_mangle]` behind `#[unsafe(..)]` and training-data recall
compiled nothing. Runtime on the map kernel: 1.5x scalar Go, below Zig —
runtime was never the objection; build, artifact, and ceremony are.

**Odin probe** (interop path): 68 MB pinned toolchain (sha-verified),
`@(export)` + `-build-mode:static` reaches Go in ~4 steps; trivial kernel
green first try — which does not test the ecosystem row, so cite that
asymmetry wherever the datapoint travels. Map kernel: 0.70 GB/s with and
without `-o:speed -no-bounds-check` — no autovectorization on that loop
shape.

## Four-criteria self-grade (per [motivation](../motivation.md))

- Correctness: goldens + 4-way byte agreement + edge statuses + race
  detector + red-proof. Strong for the tested shapes; no fuzzing.
- Agent cost: reconstructed as fix-rounds + build wall-clocks (tokens and
  session time were not instrumented — stated gap). Cheapest-evidence
  ranking observed: grep audit > codegen matrix > new-loader leg >
  new-language leg.
- Error type: 9 failures classified; 3 of 9 were stale-version recall, all
  at FFI/toolchain seams, none in kernel logic. Local `version`/help
  output beats training recall — that is a rule, not advice.
- Performance: medians, same machine/workload, ranges on re-timing
  (`build-lib` measured 0.06 s warm / 0.44 s cold — report ranges).

## Decision and revisit triggers

- No change to thresholds or hierarchy: 10% of 45% CPU still beats 3x of
  0.1%, so no fixed multiplier was adopted.
- Adopted as refinements (shipped alongside this note): SIMD verbs
  checklist, per-loader boundary priors, measured worked examples,
  portability matrix habit, per-row measured cells, subprocess price tag,
  four-criteria reporting contract.
- Revisit when: x86_64 numbers exist; Go 1.28 ships the planned
  reductions/shuffles (re-run the verbs audit); Odin 1.0 spec holds;
  a real proposal needs this as its item-5 starting point.
