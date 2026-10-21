# UI engine research (2026-10-21)

Evaluation date: **2026-10-21**. Scope: pick the UI engine and component line for
this template after the `gsx` proposal in issue #188. Numbers below were
measured on the shared ARM64 host (4 vCPU, Go 1.27.2, `GOMAXPROCS=2`), not
copied from upstream benchmarks.

## Decision

Keep **templ**. No engine outside templ/gsx beats it on the combined bar of
render speed, ready components, maturity, Datastar support, and an AI-friendly
feedback loop. For the component line, the repo keeps its skin architecture
(CSS + asset bundles) and adds a **shadcn skin**; the shadcn-templ component
library is the upstream reference, not a vendored dependency.

## Engine benchmark

Same page (20 rows of `<li><a><strong><span>` with escaped hostile text),
pooled `bytes.Buffer`, 5 samples, medians:

| engine | time | memory | allocs | note |
|---|---|---|---|---|
| quicktemplate | 4.41 µs | 0 B | 0 | fastest, but no component kit or editor tooling |
| gsx | 5.85 µs | 352 B | 21 | alpha, breaking minors |
| templ | 13.1 µs | 2.4 KB | 123 | current baseline |
| gomponents | 28.9 µs | 10.4 KB | 311 | pure Go, no codegen |
| html/template | 96.7 µs | 11.5 KB | 524 | runtime errors only |

`quicktemplate` is the only engine at or above gsx, and it is also the oldest
(2016) among the alternatives. It costs a custom syntax, no formatter, no LSP,
and no component library.

## AI-friendly feedback loop

A model correcting a wrong field (`r.Nope`) reads roughly this much output:

| engine | bytes | points at |
|---|---|---|
| gsx | 60 (210 with `--json`) | `.gsx` source |
| quicktemplate | 95 | `.qtpl` source via `//line` |
| gomponents | 107 | `.go` source |
| templ (type error) | 102 | generated `_templ.go`, not the `.templ` |
| templ (syntax error) | 369 | `.templ` source, but verbose |

Tools that matter for self-correction: a deterministic generator with a drift
check, a formatter, an LSP, a linter for the domain language, and machine-readable
diagnostics. templ has the widest set (fmt, LSP, `check-generated`,
`datastar-lint`). gsx has terser diagnostics and JSON output but a pre-1.0 API.
quicktemplate and gomponents have no markup linter; gomponents at least inherits
`gopls`, `gofmt`, `go vet`, and `staticcheck`.

## Component libraries

| library | model | maturity | Datastar |
|---|---|---|---|
| shadcn-templ (formerly templUI 2.0) | copy-in templ components, typed props, registry CLI | active, MIT, broad component set | explicit patch/swap fixes in its changelog |
| gsxui | copy-in gsx components | pre-release | components accept an attrs bag; JS is morph-aware |
| DaisyUI | CSS component classes | very mature, MIT | CSS only, no JS |
| Basecoat | CSS + small JS runtime | active, MIT | init hook needed after morph (already handled) |
| Flowbite / Preline / FlyonUI | Tailwind kits with JS | mature | need re-init after swaps; Preline-derived licenses carry a fair-use restriction |

`shadcn-templ` is a component library, **not a skin**: using it means vendoring
templ source and its JS. A skin is an asset bundle selected at runtime. The repo
therefore implements the shadcn look as a skin (tokens + component CSS over the
shared markup) instead of vendoring the library.

## Rocket (Datastar)

Rocket is Datastar's web component API, shipped as a separate
`datastar-rocket.js` bundle. It defines custom elements with typed props, setup
and render hooks, and uses Datastar for reactivity. It is beta and its API may
change.

Rocket is neither a skin nor a template engine. It is a client-side escape hatch
for behavior a server-rendered page cannot express well (pointer capture,
drag-and-drop, canvas, virtual scroll). It runs with any skin. The template does
not need it today: the whiteboard and notes already own their client behavior,
and the rest of the UI is server-rendered. Revisit Rocket for a specific
interactive surface, not as a replacement for the rendering stack.

## Revisit triggers

- shadcn-templ or another component library ships a stable, versioned release
  with a Datastar swap test.
- A page's render cost becomes measurable next to database and SSE latency.
- Rocket reaches 1.0 and a concrete widget (for example a drag-and-drop board)
  needs client-side state that the server cannot own cleanly.
