# Overview

An **agent-native product foundation in Go** that ships as **one binary**. It is
not a framework: the tree is copied once, then owned and edited — and an agent
evolves it through the `gogogo` CLI and MCP rather than through an upstream release. No external
database, no Redis, no queue service, no CDN, no JS build step. Turso
(embedded Turso Database) provides the database, owned argon2id code provides auth,
and Datastar delivers reactive UI over server-rendered HTML and SSE. Turso
Database is the driver for the web and desktop builds; the gogpu/ui window and
the opt-in Android target open the same schema with the pure-Go
`ncruces/go-sqlite3` driver (see
[the driver table](stack-layers.md#database-driver-by-target)).

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
documents how to change or remove each piece — starting with what runs and how
to switch each part off: [Features](features.md).

## Why the gates exist

A stack is only as trustworthy as the failures it catches, and the ones that
matter here are silent: they survive the build, pass review, and surface in
production. Four gates carry most of that weight — the dependency guard (no
framework creeps in one reasonable-looking import at a time), the product
boundary (the product never depends on the developer tooling), the UI linters (a
mistyped class renders with no styles and no error anywhere), and the test-value
gate (a test that cannot fail still counts as coverage).

The rest is breadth — hygiene, secrets, prose that must match the code, route
contracts, vendored drift. Where each one runs, and why the cheap ones stay in
pre-commit while the slow ones wait: [Local CI](local-ci.md).

## Who it is for

- **You who get tired of configuring the same stack over and over.**
- **You who want everything in one binary, no services to run, no
  Docker required.** One self-contained file, environment-independent.
- **You who need offline resilience.** A durable outbox + app-shell cache
  (web) and a NATS Leaf Node (desktop) let clients keep WRITING without a
  connection and replay mutations on reconnect, with server-side idempotency so
  replays never duplicate. Reads stay server-authoritative on the web (the page
  cache serves the last visited view); the desktop Leaf Node makes them local.
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

## Seven async layers, and what to remove

Most templates force you to pick **one** async strategy. This one ships all seven
in a single build and lets you opt out at runtime or delete the unit:

```text
goqite        background jobs + SSE hub delivery    (always on)
dagnats       durable multi-step workflows          DAGNATS_ENABLED=false
Loro CRDT     collaborative state, offline merges   remove internal/collab/
SSE record    visibility-scoped record-change push  (always on)
SSE ephemeral UI signals over the Datastar protocol  (always on)
JetStream     cross-instance broadcast and state    NATS_ENABLED=false
GoAkt grains  one addressable owner per room        GOAKT_ENABLED=false
```

Why each exists and when to disable it: [Seven async layers](async-layers.md).
The full dependency picture: [Stack in layers](stack-layers.md). What disappears
when you delete a unit — and the difference between *core*, *servant* and
*feature*: [Scope taxonomy](scope-taxonomy.md).

## Not a framework

Frameworks call your code and keep you on their release train. This template
does the opposite: it answers the stack decisions once, ships them wired and
documented, and gets out of the way — you rename it, you own every line, and
nothing upgrades underneath you. Upstream movement arrives as a list you read
and apply, never as a release to keep up with: [Upgrading](upgrading.md).

It does ship frameworks inside (DagNats, GoAkt, Wails), and each one is
removable per the [scope taxonomy](scope-taxonomy.md): nothing you keep is
load-bearing by accident. The claim is executable — `make check-product-boundary`
fails when the product binary imports the evolution tools — and its price is
named rather than hidden: Turso Database is pre-1.0, there is no admin UI or
generic REST to lean on, and the copy model's cost lands on whoever upgrades.
[The flat-stack decision](architecture-decision.md) ·
[costs and tradeoffs](stack-tradeoffs.md#the-flat-stack-the-cost).

## One template, six products

The same server ships as a full product, a web app, an API-only backend, or the
server behind the desktop, GUI and mobile shells. A **shape** is a closed enum
declared once at scaffold time and proved against the tree — not a per-unit
checklist. The `api` shape is the one that changes behaviour: page routes are not
mounted, so a page path answers 404 from the route tree while `/api`, `/health`
and the assets keep serving from the same binary. [App shape](shape.md).

## Try it live

A running deployment of this exact template is live at
**[gogogo.calionauta.com](https://gogogo.calionauta.com/)** — log in with a
seeded demo account when the deployment enables the demo seed
(`DEMO_ACCOUNTS_ENABLED=true`): `demo1@demo.app` / `demo1234456`;
`demo2@demo.app` takes the same password — a second browser or incognito
window makes you both at once. The admin-only env view is at [`/config`](https://gogogo.calionauta.com/config)
(sign in first) and the DagNats workflow console
at `/dagnats/` (admin **and** `ADMIN_EMAILS`; closed on a production demo; the development demo adds `demo1` to the allowlist). What each surface
gives you: [Admin & Dashboard](admin-dashboard.md).
