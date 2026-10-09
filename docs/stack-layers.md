# Stack in layers, not silos

Everything you need to build a modern web app, in a single binary. This page
explains **why each choice is here**; for the layered async topology see
[Seven async layers](async-layers.md).

| Layer | Choice | Why |
|-------|--------|-----|
| **Language** | Go 1.27 | Fast compilation, easy deploy, lean runtime |
| **Database + Auth + API** | [PocketBase](https://pocketbase.io) (embedded, on `ncruces/go-sqlite3`) | Zero-config auth, REST, admin UI at `/_/`, file storage — all in SQLite |
| **Templating** | [Templ](https://templ.guide) | Type-safe Go components, generated at build time |
| **Reactive UI** | [Datastar](https://data-star.dev) (SSE) | Server-rendered over SSE, single ~12 KiB client. CSS built once via the Tailwind v4 CLI; no JS framework build step. |
| **CSS / UI skin** | [DaisyUI v5](https://daisyui.com) (default) + TailwindCSS; pluggable: [BasecoatUI](https://basecoatui.com) | DaisyUI ~34 kB; Basecoat shadcn-style OKLCH tokens. See [UI skins](ui-skins.md). |
| **Task queue** | [goqite](https://github.com/maragudk/goqite) + SSE Hub | goqite executes with backoff; SSE Hub streams progress to the browser, no Redis |
| **Retries** | [avast/retry-go v4](https://github.com/avast/retry-go) | Exponential backoff with jitter, no boilerplate |
| **Durable workflows** | [DagNats](https://github.com/danmestas/dagnats) | Multi-step durable workflows as declarative JSON over NATS JetStream |
| **LLM SDK** | [GoAI](https://github.com/zendev-sh/goai) | Any provider: OpenAI, Anthropic, Groq, Ollama…; provider-aware retry and streaming |
| **AI credits + BYOK** | [ai-credits](https://github.com/calionauta/ai-credits) | Optional SQLite ledger: managed LLM reserve/settle billing, monthly entitlements, Stripe top-ups, encrypted per-user BYOK relay metering |
| **Real-time** | [NATS JetStream](https://nats.io) | Multi-user realtime, cross-instance broadcast |
| **Collaboration (CRDT)** | [loro-go](https://github.com/aholstenson/loro-go) | Conflict-free merging of whiteboard/notes state; converges offline edits with no last-write-wins data loss |
| **Room ownership** | [GoAkt](https://github.com/Tochemey/goakt) | One addressable owner per room: heartbeat roster plus exactly-one presenter lock, supervised |
| **Hand-drawn canvas** | [Rough.js](https://roughjs.com) (embedded) | Minimalist sketchy whiteboard rendering, embedded in the binary so it is removed with the whiteboard feature |
| **Secrets** | [age](https://age-encryption.org) + `~/.secrets/` | Local encryption, no vault, no cloud |
| **IDs** | [google/uuid](https://github.com/google/uuid) | Stable request/job IDs |
| **Live reload** | [Air](https://github.com/air-verse/air) | `make dev` regenerates templ and restarts the binary |
| **Native window (PoC)** | [gogpu/ui](https://github.com/gogpu/ui) (pure Go, zero CGO) | `cmd/gui`: a second frontend over the same PocketBase + `EntityStore` — no HTTP, no webview. See [Desktop & Mobile](desktop-mobile.md). |
| **Linting** | [golangci-lint](https://golangci-lint.run) + [datastar-lint](https://github.com/calionauta/datastar-lint) | 33 linters: `govet`, `staticcheck`, `gosec`, `revive`, `gocritic`, `errcheck`, `ineffassign`, `unused`, `errorlint`, `nilerr`, `bodyclose`, `contextcheck`, `containedctx`, `sloglint`, `thelper`, `testifylint`, `gocyclo`, `gocognit`, `funlen`, `noctx`, `goconst`, `dupl`, `lll`, `mnd`, `tagliatelle`, `modernize`, `perfsprint`, `prealloc`, `fatcontext`, `usestdlibvars`, `nolintlint`, `unparam`, `forbidigo` (no bare `time.Sleep` in production — it is uncancellable) (see `.golangci.yml`); project-specific footguns live in [`rules/rules.go`](https://github.com/calionauta/gogogo/blob/master/rules/rules.go) via `gocritic`'s `ruleguard`; `datastar-lint` catches Datastar attribute/signal/expression mistakes (run via `make datastar-lint`). The rules an agent loads: [`skills/gogogo-coding-standards`](https://github.com/calionauta/gogogo/tree/master/skills/gogogo-coding-standards) |
| **CI/CD** | GitHub Actions | `ci.yml` (lint + test + build) + `deploy.yml` (cross-compiles the prod binary and ships it to the server on `master`; `make docker-image` pushes a multi-arch image to ghcr.io, locally, when you want one). Three off-path maintenance workflows: `stelow-drift.yml` (weekly, fails when the vendored stelow standards pin is stale) and `stelow-auto-bump.yml` (weekly, opens the bump PR) and `desktop.yml` |

## Why `ncruces/go-sqlite3`?

It is the pure-Go (no cgo) SQLite engine this template standardizes on and the
always-on driver. `db/pocketbase.go` registers it as the `sqlite3`
`database/sql` driver that every query uses. PocketBase also bundles
`modernc.org/sqlite`, but that registers itself as `sqlite` and stays unused,
so **no build tag is required** and a plain `go build` just works.

Being cgo-free means clean cross-compilation of the production binary (the
deploy workflow builds `GOOS=linux` from any runner).

### Why `modernc.org/sqlite` stays in `go.mod` (read before touching)

PocketBase itself blank-imports `modernc.org/sqlite`
(`core/db_connect.go`, whose `DefaultDBConnect` opens the `"sqlite"`
driver) and version-checks it at runtime (`modernc_versions_check.go`:
warns unless driver v1.57.0 + libc v1.74.4 — the versions pinned here).
Removing it breaks the PB link, so `go mod tidy` keeps it; the
dependency is load-bearing for the framework even though our boot path
opens ncruces `"sqlite3"` exclusively. Proven by spike (both drivers
registered in one process, modernc path live). Rule: our code opens
`"sqlite3"` only — enforced by the `ModerncDriverOpen` ruleguard rule,
which fails CI on `sql.Open("sqlite", …)`. Two pools against one set
of files is the failure it prevents.

### Extensions: available, none loaded

The ncruces tree ships extensions (`fts5`, `rtree`, `vec1` — SQLite's own
vector extension — `bloom`, `unicode`, …), but nothing in this repo
registers or loads any of them (verified: no `LoadExtension`, no
`USING fts5/vec1` anywhere). The door is open — semantic search over
todos via `vec1` would be one `Register` call away — but until a feature
needs it, no extension loads. If you add one, document it here and pin
a test that queries through it: an unloaded extension directory is not
a capability.

## Why no frontend framework?

Datastar gives you reactivity with no client-side framework and no build step.
The client is a single ~12 KiB script; state lives in signals that the server
reads and writes over SSE. You get the ergonomics of a reactive UI without a
`node_modules` tree, a bundler, or hydration — and the HTML that arrives over
the wire is the same HTML the server rendered, so there is no second source of
truth to drift.

CSS is the one build step, and it is a build step you run explicitly
(`make css`), not one your users pay for at runtime: the compiled
`app.min.css` is embedded into the binary with `//go:embed`.

The rule behind the lightness: **the backend is the source of truth and
the browser holds ephemeral intent plus cache, never authority.** Records
live in PocketBase, documents in Loro snapshots, jobs in goqite — the
client renders what the server ships and reports what the user did. That
is what keeps every feature between 0 and ~48 KB of JS (measured per
feature: [Client weight](features.md#client-weight)). The honest boundary:
offline outboxes and textarea drafts do live client-side, but as cache to
be reconciled, never as truth to be defended.

## Why this one (and not that one)?

One-line reasons live in the table. This section records the cost behind
each choice — what was rejected, what it costs to keep, and when to
revisit.

### Go (and not TypeScript / Python)?

LLMs reach for TypeScript and Python by familiarity, not fitness — the same
flywheel as React: TS grows on React/Next's back, Python on AI's (+7% in
2025). Both are defensible defaults elsewhere; here they lose on three
structural points. One static binary (~30 MB on `scratch`) against a runtime
plus `node_modules` plus a build step (Node) or an interpreter plus venv
plus a packaging matrix (Python). Concurrency as the default (goroutines)
against a single-threaded event loop (Node) or a GIL that in 2026 means
running a second interpreter (3.14t, opt-in) whose lock silently re-enables
on any unmarked C extension, with no stable ABI and no official container
tag yet. Millisecond builds and `gofmt` uniformity, which is what makes
agent output checkable before it runs. Rejected: Node (single thread, build
step per deploy, and an install-time code-execution primitive — npm
`preinstall`/`postinstall` hooks run on `npm install` before any import,
which is the delivery mechanism behind the 2026 worm waves in `keyv`,
`axios`, 140+ `@mastra/*` packages, and 639 `@antv` versions; Go has no
equivalent hook, resolves through a checksum-backed proxy, and never
executes dependency code at build time), Python (packaging converged with
`uv` plus lockfiles in 2026 — no strawmanning `pip` — but the interpreter
matrix plus the GIL migration remain, and raw speed is an order of
magnitude off). Cost: a smaller hiring pool than TS, no ML/data ecosystem
to speak of, slower raw single-core than C/Zig/Rust (a May-2026
cross-language suite puts Go ~2x off C and CPython ~40x on synthetic
workloads — direction, not destiny). Revisit per workload: Python the day
the app is ML/data-first (numpy/pandas have no Go equivalent worth
fighting), TypeScript the day the team or the product is JS-only and the
npm ecosystem pays the binary's rent.

### goqite + retry-go (and not Redis)?

A queue without a service to run: jobs persist in the SQLite the app
already owns, retries back off with jitter, and one binary stays one
binary. Rejected: Redis-style brokers (a second process to persist, secure,
and operate) and JetStream as a job queue (it is the cross-instance
broadcast, not a work queue with per-job retry semantics). Cost:
single-process throughput — depth and worker latency are bounded by this
box. Revisit when profiles show the queue, not the work, as the bottleneck.

### DagNats (and not Temporal)?

Workflows as declarative JSON, embedded in the binary, state durable on the
JetStream the template already runs. No cluster to install, no second
surface to learn. Rejected: Temporal (a server plus database plus SDKs for
durability most apps never need at this scale). Cost: the expressive and
throughput ceiling of a young JSON engine. Revisit when a workflow outgrows
JSON, needs multi-tenant operation (namespaces, per-team RBAC and audit —
none documented upstream), or needs a hosted offering.

### JetStream on by default (and not opt-in)?

Durable workflows, cross-instance broadcast, and collab sync all assume it,
so on-by-default keeps every feature working out of the box — and one env
var switches it off. Cost: an embedded NATS server in every boot, its ports
and storage in every deploy. Strictly single-instance with no workflows?
`NATS_ENABLED=false` removes the cost entirely.

### GoAI (and not the provider SDKs)?

One injectable interface over any OpenAI-compatible provider, with a stub
(`internal/llm/fakeserver`) that keeps real LLMs out of tests. Rejected:
one SDK per vendor (a dependency each, untestable without network). Cost:
the wrapper lags newest provider features and adds its own surface to
learn. Revisit per provider when it blocks a capability you actually call.

### Loro, server-owned (and not Yjs / Automerge / OT)?

The merge engine with the smallest client footprint: none. The server owns
the Doc, browsers POST ops and render the JSON broadcast back — no JS CRDT
library, no npm build step — and offline edits still merge without
last-write-wins. Rejected: Yjs in the browser (a second CRDT system doing
Loro's job, plus a bundler, plus a sync protocol that fits websockets
better than the SSE + NATS transport here); Automerge (same overlap, same
cost); OT in the style of the Datastar collab demo (server-serialized,
plain text only, no offline merge — honest for notes, insufficient for
rich text). Cost: every keystroke round-trips — no client-local editing —
and the rich-text editor binding (cursor mapping, marks) is future work,
as a `removal=feature` package reusing `internal/collab`, never a new
plugin.

### age + `~/.secrets/` (and not Vault / KMS)?

Secrets as encrypted files, decrypted at boot: zero services, zero bills,
zero IAM. Rejected: Vault / cloud KMS (a service to run and pay for, so a
single box can hold its own secrets). Cost: no rotation story, and
distribution across instances is manual. Revisit when secrets outgrow one
box or need audit trails.

### Wails + gogpu/ui (and not Electron / Tauri)?

Desktop shells over the same backend: Wails for a webview shell with
offline edge sync, gogpu/ui for a pure-Go window with no webview and no
HTTP at all. Rejected: Electron (a Chromium per app, its own update and CVE
surface). Cost: the platform matrix and native debugging stay yours. Both
are delete-with-the-package features, never core.

### 30+ linters (and not `go vet` alone)?

Tuned for agent-written code: unchecked errors, leaked contexts, unclosed
bodies, weak crypto, template-signal mistakes — enforced in pre-commit
hooks and CI, taught upfront via the coding-standards skill. Cost: CI
minutes and a `//nolint` discipline curve. Revisit by deleting rules that
never fire, not by adding more.

### PocketBase (and not chi/gin + Postgres, or Supabase/Firebase)?

Database, auth, REST, file storage, realtime, and an admin UI in one
embedded binary — maintained upstream for years, speaking plain SQLite
underneath. Rejected: assembling it yourself (router + SQL + session auth +
an admin you write and secure), and outsourcing it (Supabase/Firebase move
your data behind a network call, a bill, and someone else's changelog).
Cost: PocketBase shapes the app — collections model, its Go API surface,
the admin/superuser split, SQLite's single-writer ceiling. Revisit when the
write load or the data model outgrows one embedded database.

#### Postgres (and Supabase) — when it actually wins

Postgres is the #1 database in the 2025 Stack Overflow survey (58%) and
Supabase (13M+ developers) just bought Turso (Oct 2026) with an explicit
thesis: SQLite to start, Postgres to scale, with a graduation path between
them. Take that thesis at face value — it is also our revisit trigger.
Postgres genuinely wins when: sustained multi-writer contention on the same
rows, true horizontal writes across regions, or heavy analytics the app
itself must run. Until profiles prove one of those, "only Postgres scales"
is premature: a single NVMe box runs SQLite/WAL at 100k+ reads/s and ~10k
writes/s, readers never block the writer, and the first rungs of the ladder
are batching (one transaction, not N commits), a generous `busy_timeout`,
and a write queue — not a second database server.

#### Scaling PocketBase's own SQLite — the honest ladder

Upstream's stance is single-server vertical: 10k+ persistent realtime
connections on a $4 VPS, a 120-connection read pool against a 1-connection
write pool with built-in `database is locked` retries. That covers small to
midsize SaaS without trying. Past that, in order:

1. **Disaster recovery** — Litestream streams WAL to S3-compatible storage
   (v0.5 added LTX + VFS read replicas for moderate loads). Restores in
   seconds; no app changes.
2. **Read distribution** — libsql embedded replicas serve reads from a local
   file at local-disk speed while writes forward to a primary. Caveat that
   kills it for us today: embedded mode needs CGO, which conflicts with the
   cgo-free `ncruces` stance that keeps cross-compilation clean (see [Why
   `ncruces/go-sqlite3`?](#why-ncrucesgo-sqlite3)). Revisit only with the
   unified build.
3. **Community HA** — `pocketbase-ha` (NATS replication, last-writer-wins or
   static leader), libsql primary-replica PoCs, Marmot+NATS multi-primary.
   All third-party, all with the same asterisk: hooks and realtime are
   evaluated in-process, so they do not sync across nodes the way records
   do. Leader-for-writes is the only consistent topology; multi-primary is
   eventual consistency wearing a costume.
4. **Graduation** — a Postgres `EntityStore` strategy behind the existing
   `features/store` interface, plus replacements for the PB pieces the
   interface does not cover (auth, REST, realtime, admin UI). That is a
   project, not a flag flip — which is why the trigger is measured write
   load, not anxiety.

Write-heavy-global (payments, bidding, telemetry at planetary scale) skips
the ladder and starts at Postgres. Everything else should have to prove it
needs to leave.

#### Cloud counterparts, if the box ever stops being enough

Turso (embedded replicas, per-tenant databases, MVCC concurrent writes still
in beta — not a bet this quarter), Cloudflare D1 (zero-config inside
Workers, ~10 GB ceiling, Workers-only), Supabase Postgres (the graduation
destination, now with Turso inside it). None of them change the default:
the app runs whole on one box with zero cloud dependencies until the ladder
above says otherwise.

#### Routers: PocketBase rides the stdlib, and that ends the debate

Since v0.23 PocketBase dropped Echo and routes on a thin wrapper over Go's
`net/http.ServeMux` — Gin (89k stars, zero-alloc radix, custom context,
12 deps) and Echo (33k stars, JSON/p99 balance, 8 deps) beat it on
microbenchmarks, and Chi (22k stars, zero deps, stdlib-pure — the one
Supabase's own auth service uses) is the minimalist pole. None of it
matters here: routing is microseconds against milliseconds of SQLite, and
PocketBase's maintainer says exactly that — ignore artificial benchmarks,
apps bottleneck on DB operations. There is no router upgrade that moves our
numbers; the day routing shows up in a profile, the database has already
been the problem for months.

### Datastar (and not htmx, and not a JS framework)?

Datastar's own guide puts it plainly: backend reactivity like htmx plus
frontend reactivity like Alpine, with no npm packages. Signals hold state,
the backend drives DOM patches, and SSE streaming is a first-class
primitive — unlike either. htmx overlap is real and acknowledged: simple
hyperlink-style swaps stay simpler in htmx. What Datastar adds is the
realtime half Delaney keeps pushing beyond htmx: persistent SSE streams the
backend owns, which is exactly what the SSE Hub and the collab features
stand on. Rejected: SPA frameworks (a bundler, hydration, a second source
of truth in the client) for the default path. Cost: a younger, smaller
ecosystem than React; persistent connections your proxies must allow; more
logic lives in the backend by design. The repo pays down the newness with
`datastar-lint` and the `internal/datastar` wrapper.

#### React/Next: the flywheel is real, and so are its costs

State of React 2025 names the mechanism honestly: React's ecosystem mass
means more training data, which makes AI better at React, which brings more
developers, which makes more data (React ~47%, Next.js ~21% in the 2025
Stack Overflow survey). An LLM reaching for Next.js is usually reaching for
familiarity, not fitness — the advantage holds regardless of technical
merit, which is exactly why it should not decide the stack.

The costs are structural, converging across independent 2026 production
reports: a misplaced `use client` pulls its whole subtree into the client
bundle; server/client boundaries multiply cache, transport, and debugging
surface (origin CPU per request, Flight payloads, hydration roots); and the
apps that pay most — dashboards, admin panels, realtime collaboration, the
exact shape of this template's features — gain least, because interactivity
everywhere collapses the server-first advantage while keeping its overhead.
Datastar is 11.29 KiB full (~5 KiB core): signals, computed, effects, and
SSE `patchElements`/`patchSignals` — everything realtime needs, no npm, no
build step, no second source of truth to drift. And where React relies on
discipline, this repo relies on a check:
[datastar-lint](https://github.com/calionauta/datastar-lint) fails the
build on signal/attribute/expression mistakes in seconds — the same
cheap-failure bet as the rest of the stack.

### Demo features shipped (and not a bare core)?

`todo`, `whiteboard`, `room`, `notes` ship as working pages — not because
every app needs a todo list, but because patterns teach better than prose:
todo is the reference MVC, whiteboard the CRDT canvas, room the actor
pattern, notes the collaborative text. Rejected: a minimal core with
nothing to copy (every scaffold would re-derive the same wiring, badly).
Cost: scaffold weight and trim burden — each demo is a unit the installer
offers to strip (`whiteboard`, `goakt`, `landing`, `config-view`,
`credits`, `sounds`, `skins`) or a documented manual removal (`todo`,
`notes`), and every demo rots if its pattern drifts from the core. Revisit
per demo: anything nobody copies in real scaffolds is deletion candidate,
not heritage.

## How the pieces combine

The layers are not independent choices — they are designed to work together:

- A handler returns a **Templ** component; **Datastar** decides whether the
  response is a full page or an SSE patch.
- Background work goes to **goqite**; the worker streams a **toast** back to
  the originating browser through the **SSE Hub**, addressed by `clientID`.
- Multi-step processes go to **DagNats**, whose state is durable on **JetStream**.
- Collaborative state goes through **Loro CRDT**, so offline edits merge
  without last-write-wins data loss.
- Record changes fan out through **PocketBase realtime**, scoped per user by
  the collection's own access rules.
- Single-owner decisions go to **GoAkt grains**: one addressable owner per room (roster, locks, supervision).

## Related

- [Features](features.md) — every capability and its runtime opt-out.
- [Async layers](async-layers.md) — the seven layers and why each exists.
- [Code quality for LLM agents](code-quality.md) — the lint set behind these choices.