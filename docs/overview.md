# Overview

A full-stack Go web app template that ships as **one binary**. No external
database, no Redis, no queue service, no CDN, no JS build step. PocketBase
(embedded SQLite) provides the database, auth, REST API, file storage, and
admin UI; Datastar delivers reactive UI over server-rendered HTML and SSE.

Built to be useful: every decision favors practical outcomes over abstract
ideals. The stack optimizes for simplicity, consistency, and shipping software
with minimal friction.

## The problem it solves

Every web project starts with the same conversation — pick a database, auth,
router, reactive frontend, task queue… — and the project stalls at the
*decisions*, installations, and configurations. Not at the code.

This template answers those decisions once, ships them wired together, and
documents how to change or remove each piece.

## Who it is for

- **You who get tired of configuring the same stack over and over.**
- **You who want everything in one binary, no external dependencies, no
  Docker required.** One self-contained file, environment-independent.
- **You who need offline-first resilience.** A Service Worker + Background Sync
  queue (web) and a NATS Leaf Node (desktop) let clients keep working without
  a connection and replay mutations on reconnect, with server-side idempotency
  so replays never duplicate.
- **You who prefer one language for the whole stack and reactive frontend
  without heavy frontend frameworks.** Server-rendered HTML via SSE —
  lightweight, no bloated SPA, no JS build step.
- **You who want a language predictable for both humans and LLMs.** Go's
  syntax is minimal and consistent. Same formatting everywhere (`gofumpt`).
  Static typing catches whole classes of bugs at compile time. Native
  concurrency (goroutines + channels) is easy to reason about — no
  async/await chains, no callback pyramids. The codebase is equally readable
  by you, your team, and AI coding agents.
- **You who care about supply chain security.** Go has no mass npm-style
  dependency tree. Every module is verified by content hash (`go.sum`).
  Built-in vulnerability auditing (`govulncheck`) scans the dependency graph
  for known CVEs. No transitive dependency hell.
- **You who want an LLM client wired in without pulling in a whole
  orchestration framework.** `internal/llm` wraps GoAI (any OpenAI-compatible
  provider) behind an injectable interface, callable from handlers. It calls a
  *remote* provider API; it is **not** a local-model runtime.

## What makes it different

Most templates force you to pick **one** async strategy. This one ships **six**
complementary layers in a single build and lets you opt out at runtime:

| Layer | Solves |
|---|---|
| `goqite` | background jobs + SSE hub |
| `dagnats` | durable multi-step workflows as declarative JSON |
| Loro CRDT | conflict-free collaborative state |
| PocketBase realtime | per-user-scoped record-change push |
| SSE Hub | ephemeral signals over the Datastar protocol |
| JetStream | cross-instance broadcast and state |

See [Six async layers](async-layers.md) for why each exists and when you
should disable it, and [Stack in layers, not silos](stack-layers.md) for the
full dependency picture.

## Try it live

A running deployment of this exact template is live at
**[gogogo.calionauta.com](https://gogogo.calionauta.com/)** — log in with the
seeded demo account (`demo@demo.app` / `demo1234456`). The PocketBase admin dashboard
is at [`/_/`](https://gogogo.calionauta.com/_/) and the DagNats workflow console
at [`/dagnats/`](https://gogogo.calionauta.com/dagnats/). What each surface
gives you: [Admin & Dashboard](admin-dashboard.md).

## Related

- [Getting started](getting-started.md) — create your repo from the template, rename it, run it.
- [Architecture](architecture.md) — how the layers depend on each other.
- [Features](features.md) — every capability and its runtime opt-out.
- [Configuration](configuration.md) — every environment variable.