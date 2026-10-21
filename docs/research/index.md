# Research, comparisons and tradeoff decisions

This is the single entry point for every research note, comparison and
tradeoff decision in the repo. Each page records what was evaluated, when, and
what was chosen, so a later reader can tell a current decision from a past one.

Use this hub when you are about to change a foundational choice, or when you
need the reasoning behind one. The implementation docs are linked from the
individual notes.

## UI and frontend

| page | reviewed | answers |
|---|---|---|
| [UI engine research](ui-engine-research.md) | 2026-10-21 | templ vs gsx vs quicktemplate vs gomponents vs html/template: render performance, diagnostics and forkability |
| [UI components comparison](ui-components-2026-10-21.md) | 2026-10-21 | DaisyUI vs Starbase vs datastar-ui vs dsx: Datastar/SSE fit, lightness, functionality, maintainability |
| [UI skins](../ui-skins.md) | living | the chosen skin architecture and how DaisyUI, Basecoat and shadcn coexist |

## Stack and runtime tradeoffs

| page | reviewed | answers |
|---|---|---|
| [Stack tradeoffs](../stack-tradeoffs.md) | living | what each core choice rejected, what it costs to keep, and when to revisit it |
| [Native language tradeoffs](../native-language-tradeoffs.md) | living | when a native kernel is worth it, and how to decide without preference |
| [Exception to Go](../exception-to-go.md) | living | the Go-first rule, the Zig exception, and the benchmark bar |
| [DB concurrency](../db-concurrency.md) | living | Turso single-writer stance, the probed MVCC path, and revisit triggers |

## Decisions and rationale

| page | reviewed | answers |
|---|---|---|
| [Architecture decision](../architecture-decision.md) | living | the flat-stack decision record: what it serves, what it replaced, and the migration it completed |
| [Inspirations](../inspirations.md) | living | what this project learned from other projects, credited rather than vendored |
| [Motivation](../motivation.md) | living | the thesis behind the stack and how to choose without hype |

## How to add a research page

1. Put the file under `docs/research/` when the subject is an evaluation,
   comparison or tradeoff; put implementation docs next to the thing they
   document.
2. Start with a title and a `Reviewed: YYYY-MM-DD` line (or `living` when the
   page is maintained instead of dated).
3. Add the page to this hub and to the site MANIFEST in `site/build.mjs`.
4. Record the decision and its revisit trigger, not only the winner. A
   comparison without a trigger becomes stale silently.
