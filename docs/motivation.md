# Go for almost everything, zig or rust for the exception

> A thesis, not a rule. Written Oct 2026, while the timeline argued about
> which language pairs best with LLMs. The enforceable rules live in
> [exception-to-go](exception-to-go.md); this page explains why they exist.

## The 99-to-1 bet

Anyone following the timeline chatter in Sep–Oct 2026 has seen the running
argument about which languages pair best with LLMs and performance. Rust has
been the flavor of the month.

It has strong points. But it does not need to be the default for everything
— nor is it the only alternative. Judge a language by context, with actual
criteria:

- correctness: the program does exactly what was asked, byte by byte, and flags the error instead of crashing on invalid input.
- agent cost: what it took to get there — conversation volume (tokens), back-and-forth rounds (attempts), and the clock (total time, fix time, build time).
- error type: when something went wrong, was it grammar, an outdated version, logic, or AI hallucination?
- performance: operations per second (median across runs, not one sample) and the actual gain when optimization was requested.
- coexistence: the cost of living with the rest of the system — translation between languages, deployment environments, a fallback plan, and a clean exit if you ever strip it out.

My take: LLMs need a language where failing is cheap — errors surface early
and fixes are cheap — and humans can read whenever they step in.

That covers 99% of today's web systems and services, leaving 1% for another
language chosen by context (like zig or rust — and where it fits, odin or
mojo). The split is illustrative, not measured: shorthand for "almost
everything, then a rare exception", not a benchmark. The criteria above
confirm the thesis as well as they refute it — and I could still be wrong,
since everything moves too fast for certainty. But it is a cheap bet to
test.

Why Go? Compatibility is promised — code from a decade ago still compiles,
so AI trained on old code stays valid. The toolchain fixes things for free:
gofmt, vet, and millisecond builds close the loop on every error before
running. Lints catch typical AI-generated mistakes. And agent-era infra
already runs on it: kubernetes, ollama, temporal, and the like.

I also wanted something extremely light on the client with the backend as
the source of truth: kilobytes of JS per feature, never authority in the
browser — records in the database, documents in CRDT snapshots, intent
posted as ops. The offline outbox is cache to be reconciled, not truth to
be defended; that single rule is what keeps the whole client featherweight
without giving up realtime collaboration.

## Why this became gogogo

A single-binary fullstack in Go, dev-ready and ai-ready: a CLI and MCP so
the agent knows what to use without guessing or burning tokens searching,
plus a skill with the Go coding standards.

In gogogo the exception is not an open choice: zig is the named default for
a native kernel, anything else needs a written case — the decision is made
calmly, once, not re-derived by every agent under deadline pressure. The
procedure and the removal plan are in [exception-to-go](exception-to-go.md).

Install it, ask the LLM to build, no external services. Core is opinionated;
plugins and features strip away at install. The live capability list is in
[features](features.md).

## Core: opinionated

The foundation — swapping any of this means a fork, not a toggle. Chosen by
the criteria above:

- **PocketBase (embedded SQLite)** — database, auth, REST, file storage, admin UI. Zero config, zero separate service.
- **Templ + Datastar over SSE** — server-rendered reactive UI, ~12 KiB client, no JS framework build step.
- **goqite + SSE Hub (+ retry-go)** — background jobs with exponential backoff; progress streams to the browser. No Redis.
- **age + `~/.secrets/`** — secrets encrypted at rest, decrypted at boot. No vault, no cloud.

## Plugins and features: disable or remove whatever you want

Plugins switch off with an env var; features delete with the package.
Snapshot of Oct 2026 — the live list with opt-outs is
[features](features.md):

- **DagNats over JetStream** — durable multi-step workflows as declarative JSON; kill mid-run and it resumes.
- **NATS JetStream** — broadcast across instances behind a load balancer.
- **GoAI (+ optional ai-credits)** — LLM calls on any provider; optional credits ledger and BYOK billing.
- **Service Worker + NATS Leaf Node** — offline-first sync; queued edits replay without duplicating.
- **GoAkt grains** — one addressable owner per room: heartbeat roster plus exactly-one presenter lock, supervised.
- **Wails v3 + gogpu/ui** — desktop shells over the same backend.
- **DaisyUI v5 + Basecoat** — two UI skins compiled into one binary, switchable at runtime.
- **Todo app (PocketBase realtime)** — the reference app: full CRUD with per-user live updates. Copy the pattern, then delete it.
- **Whiteboard (Loro CRDT + Rough.js)** — conflict-free collaboration with offline merge.
- **Room presence demo (GoAkt)** — heartbeat roster plus presenter lock, to copy for per-entity features.

## Collaboration: still early, moving fast

The repo is open and under active development — expect rough edges. That is
exactly why usage reports matter most right now: if you build something real
on it, tell me what broke. An issue with a reproduction is worth more than a
PR. The rules are in
[CONTRIBUTING](https://github.com/calionauta/gogogo/blob/master/CONTRIBUTING.md).

## Related

- [Overview](overview.md) — what gogogo is and who it is for.
- [Stack in layers](stack-layers.md) — every dependency and why it is in the box.
- [Native boundary](exception-to-go.md) — the enforceable Go-first policy behind this thesis.
- [Features](features.md) — every capability and its runtime opt-out.
