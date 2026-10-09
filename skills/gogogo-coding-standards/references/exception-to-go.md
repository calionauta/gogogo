# Exception-to-go gate (Zig; summary — normative doc is `docs/exception-to-go.md`)

There is no Zig code and no Zig toolchain in this repo today. That is the successful default, not a gap. This file is the 80-line gate agents check; the full decision procedure, boundary rules, and removal plan live in `docs/exception-to-go.md` and win on conflict.

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
- **Lifetime/leak gate (run, don't eyeball).** `alloc` immediately paired with `defer`/`errdefer free`, same allocator frees, no global allocator. Tests use `std.testing.allocator` or `DebugAllocator` asserting `deinit() == .ok`, plus error paths forced and a ~10k-call loop test. Paste `zig build test` (safety on) + `go test -race -count=1` green; `.leak` rejects the kernel.
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

## API truth vs. style guidance (read before trusting any Zig skill)

**There is no official Zig skill published by the Zig team.** `ziglang.org`
ships release notes and the language reference, nothing agent-shaped. So a
third-party skill is *never* the source of truth for an API — it is a snapshot
of someone's reading of one release, and Zig breaks APIs between releases more
than most languages.

| Source | Status | Use it for |
|---|---|---|
| **ZLS** ([zigtools/zls](https://github.com/zigtools/zls)) | community-maintained (the `zigtools` org, not `ziglang`), and the tooling the Zig community points at; releases are version-matched to Zig | **API truth.** Diagnostics, completions, go-to-definition, hover. If ZLS disagrees with a skill, ZLS wins — it reads the actual compiler. |
| `zig env` + `<lib_dir>/../doc/langref.html` + `.std_dir` sources | ships with the toolchain | **API truth for the pinned version.** Local and exact. |
| `zigcc/skills` (the table above) | **community project** — zigcc is the Chinese-language Zig community at ziglang.cc, not `ziglang` | **Style and orientation.** Idioms, migration shape, what to expect to have changed. Verify every signature against ZLS before writing it. |
| `nzrsky/zig-skills`, `full-stack-skills/zig-skills`, similar | individual / AI-org projects | Same as above with less track record. Prefer `zigcc/skills` when pinning a version skill. |

Concretely: when an agent writes a Zig call, it confirms the signature with ZLS
or the local std sources. A vendored skill shortens the search; it does not end
it. If the host exposes ZLS over MCP, prefer that over any skill text.
