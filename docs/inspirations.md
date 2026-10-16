# Inspirations

What gogogo learned from other projects. Credit, not compatibility:
ideas below were re-implemented in gogogo's own architecture, not vendored.

## northstar by Nicholas Zanghi (Zangster)

Repo: https://github.com/zangster300/northstar (MIT)

The original scaffold gogogo started from: Go + NATS + Datastar +
Templ + DaisyUI as one boilerplate for realtime hypermedia apps.

What carried over:

- Stack choice: server-rendered hypermedia with Datastar instead of
  a JSON API + SPA.
- Embedded NATS for realtime (northstar embeds NATS in `cmd/web`;
  gogogo embeds NATS JetStream + Turso Database in one binary).
- `features/` + `router/` + `config/` layout and Air/esbuild live reload.

What diverged: gogogo added Turso Database persistence, goqite
background jobs, DagNats durable workflows, offline sync with Service
Worker + IndexedDB outbox, installer/MCP/CLI, and a trim-by-deleting
template model. Northstar stays a minimal starter.

## The 2026 generative-UI standards (A2UI, OpenUI Lang, json-render, AG-UI)

Generative UI settled into a recognizable literature during 2026: Google's
**[A2UI](https://a2ui.org/)** (Apache 2.0, JSON messages
`createSurface`/`updateComponents`/`updateDataModel`, flat adjacency-list
component model, catalog advertised to the model), Thesys
**[OpenUI](https://github.com/thesysdev/openui)** (line-oriented DSL, the
cheapest wire format measured), Vercel Labs
**[json-render](https://github.com/vercel-labs/json-render)** (JSON Patch
against a flat element tree), CopilotKit's
**[AG-UI](https://docs.ag-ui.com/)** (~16 event types over SSE/WS for
agent↔frontend), and the
[State of Generative UI report](https://www.openui.com/blog/state-of-generative-ui-report)
that names the Static → Declarative → Open-Ended spectrum we sit on.

What carried over (no dependency, no vendored code — the shape only):

- **Static + a sandboxed escape hatch in the same catalog** is the
  pattern the report recommends; our Ask (catalog-only) and Chat
  (`html_block`) already are that pair, with the escape hatch constrained
  by sanitizer + class vocabulary instead of an iframe.
- **Catalog advertised to the model** so "advertised" and "resolvable"
  cannot drift — A2UI v0.9's "prompt-first" turn, which is what
  `PromptForCatalog` had been doing.
- **Flat component list with id references**, which is what a
  `Directive{Component, Props, Children}` already is.
- **Validate at runtime, repair bounded, fall back deterministically**,
  and keep the failure countable.
- The rules an agent must follow live in
  `skills/gogogo-coding-standards/references/genui.md`.

What we deliberately did **not** take: the protocols as dependencies.
AG-UI and MCP Apps solve distribution into hosts you do not own (ChatGPT,
Claude, IDE copilots) and third-party agent frameworks; we are one Go
binary rendering its own Templ through Datastar SSE, where an event
protocol would add a layer and remove the server-side renderer that makes
our render path a pure function. The mapping table in the skill reference
is what keeps an A2UI adapter a thin function later.

