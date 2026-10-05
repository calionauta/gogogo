# Zig gate (summary — normative doc is `docs/native-zig.md`)

There is no Zig code and no Zig toolchain in this repo today. That is the successful default, not a gap. This file is the 80-line gate agents check; the full decision procedure, boundary rules, and removal plan live in `docs/native-zig.md` and win on conflict.

## Bans (never sufficient alone)

"Zig is faster", manual memory, "more low-level", "could be optimized", "looks like SIMD", preference, avoiding a normal Go dep, hypothetical future perf.

## Decision (first "Go" wins)

1. Normal product logic → Go. HTTP/auth/DB/realtime/jobs/LLM/UI → Go.
2. No demonstrated hot path → Go.
3. Go stdlib/compiler/`simd`+`archsimd`/established lib solves it → Go.
4. No profile/bench vs Go baseline, or no measurable benefit → Go.
5. Else → write the 11-point justification (problem, Go baseline, evidence, benefit, why Zig, C-ABI boundary, portability, build/CI, maintenance, fallback, removal) or stay in Go.

## If a kernel ever lands

- One kernel, one owner (`internal/<thing>/zig/`), small C ABI, caller-owned buffers, who-allocates/who-frees documented, no Zig types/allocators leaking into Go, stateless where possible.
- Pure-Go fallback ships from day one; deletion leaves a working binary (`go build ./cmd/web` proves it).
- Pinned toolchain, locked inputs, `scratch`-static safe, scoped cached CI step. No top-level `native/` forest, no shared "zig utils", no Zig framework, stdlib first.
- Tests: golden vectors vs Go baseline + edges (empty/max/malformed) + lifetime tests + bench on same workload + hand-mutation red-proof + fuzz on untrusted inputs.
- Debug story: symbols across the boundary + Go-vs-Zig bisection documented in the package.

## Local-docs-first (when Zig exists)

`zig env` for `.lib_dir`/`.std_dir`/`.version` — never hardcode paths. Check `<lib_dir>/../doc/langref.html` + `.std_dir` sources + `zig std` server before web search.

## What to vendor, and when

Nothing today. When the first kernel passes the gate, vendor exactly one pinned skill matching the toolchain and reference it from the justification:

| Toolchain | Vendor | Covers |
|---|---|---|
| 0.15.x | `zigcc/skills --skill zig-0.15` | pre-Io baseline |
| 0.16.0 | `zigcc/skills --skill zig-0.16` | `std.Io`, `@Type` removal, `@cImport`→`addTranslateC`, `ArrayList.initCapacity`, `Thread.Pool`→`Io.Group`, Juicy Main, `build.zig.zon` fingerprint, `zig-pkg/` |
| 0.17.x | `zigcc/skills --skill zig-0.17` | 0.16 + 0.17 migration notes |

Never float latest, never vendor two versions, never use a 0.17-dev snapshot for stable work. Style: add `zig-tiger-style` only alongside the pinned version skill.
