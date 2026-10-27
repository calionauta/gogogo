# Overview

A full-stack Go web app template that ships as **one binary**. No external
database, no Redis, no queue service, no CDN, no JS build step. Turso
(embedded Turso Database) provides the database, owned argon2id code provides auth,
and Datastar delivers reactive UI over server-rendered HTML and SSE.

That list is the point: the template replaces the **infrastructure you would
have to operate**. Providers you choose to call — an LLM, a payment API, an
SMTP server, an OAuth identity provider — stay external by nature, sit behind a
config switch, and never block a fresh clone from booting.

Built to be useful: every decision favors practical outcomes over abstract
ideals. The stack optimizes for simplicity, consistency, and shipping software
with minimal friction.

## The problem it solves

Every web project starts with the same conversation — pick a database, auth,
router, reactive frontend, task queue… — and the project stalls at the
*decisions*, installations, and configurations. Not at the code.

This template answers those decisions once, ships them wired together, and
documents how to change or remove each piece.

## Not a framework

Frameworks call your code and keep you on their release train. This template
does the opposite: it answers the stack decisions once, ships them wired and
documented, and gets out of the way — you rename it, you own every line, and
nothing upgrades underneath you. Upstream movement arrives as a list you read
and apply, never as a release to keep up with: [Upgrading](upgrading.md). It
does ship frameworks inside (DagNats, GoAkt, Wails), and each of them is
removable per the [scope taxonomy](scope-taxonomy.md): nothing you keep is
load-bearing by accident. For an LLM agent this means zero new DSL to learn —
plain Go, stable third-party surfaces, and the skill files, instead of a moving
proprietary framework. The claim is about your code, not about the dependency
graph: nothing sits between you and the handlers, and what ships alongside is
listed as removable units rather than hidden behind an API.

## Who it is for

- **You who get tired of configuring the same stack over and over.**
- **You who want everything in one binary, no services to run, no
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
  for known CVEs; a scheduled Syft + Grype workflow keeps an SBOM and scans
  the built image. No transitive dependency hell.
- **You who want an LLM client wired in without pulling in a whole
  orchestration framework.** `internal/llm` wraps GoAI (any OpenAI-compatible
  provider) behind an injectable interface, callable from handlers. It calls a
  *remote* provider API; it is **not** a local-model runtime.

## What makes it different

Most templates force you to pick **one** async strategy. This one ships **seven**
complementary layers in a single build and lets you opt out at runtime:

| Layer | Solves |
|---|---|
| `goqite` | background jobs + SSE hub |
| `dagnats` | durable multi-step workflows as declarative JSON |
| Loro CRDT | conflict-free collaborative state |
| SSE record stream | visibility-scoped record-change push + re-list |
| SSE Hub | ephemeral signals over the Datastar protocol |
| JetStream | cross-instance broadcast and state |
| GoAkt grains | one addressable owner per room (roster, locks, supervision) |

See [Seven async layers](async-layers.md) for why each exists and when you
should disable it, and [Stack in layers, not silos](stack-layers.md) for the
full dependency picture.

## Try it live

A running deployment of this exact template is live at
**[gogogo.calionauta.com](https://gogogo.calionauta.com/)** — log in with a
seeded demo account when the deployment enables the demo seed
(`DEMO_ACCOUNTS_ENABLED=true`): `demo1@demo.app` / `demo1234456`;
`demo2@demo.app` takes the same password — a second browser or incognito
window makes you both at once. The admin-only env view is at [`/config`](https://gogogo.calionauta.com/config)
(sign in first) and the DagNats workflow console
at `/dagnats/` (admin **and** `ADMIN_EMAILS`; closed on the public demo). What each surface
gives you: [Admin & Dashboard](admin-dashboard.md).

## Related

- [Motivation](motivation.md) — the thesis: Go for almost everything, Zig as the named exception.
- [Getting started](getting-started.md) — create your repo from the template, rename it, run it.
- [Architecture](architecture.md) — how the layers depend on each other.
- [Features](features.md) — every capability and its runtime opt-out.
- [Security model](security.md) — sessions, RBAC, audit, CSP, secrets and residual risks.
- [Configuration](configuration.md) — every environment variable.