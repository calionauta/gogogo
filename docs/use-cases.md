# Use cases — what each core / plugin / feature is for

Who this is for: you (or your agent) described a product need and want to
know which pieces of the template carry it — before running `advise`,
and in business language rather than registry ids. Each entry names
canonical cases it **enables** and one line it **is not for**.
Switch-off mechanics (env flag vs deletion) are in
[scope-taxonomy](scope-taxonomy.md#removing-a-component); this page is
about *fit*, not removal.

Conventions: **core** is load-bearing (customize, never remove);
**plugin** serves other capabilities (nothing user-facing disappears, but
something stops working); **feature** is a terminal user surface (a page
or journey disappears when trimmed).

## Core

### `queue` — background jobs, retry, SSE fan-out (core)

What: goqite-backed persistent jobs + worker pool with retry
classification, plus the SSE hub that pushes updates to browsers.

- Nightly email reports, webhook retries with backoff, thumbnail
  generation, CSV exports — anything over ~50ms that must survive
  a restart.
- Live progress bars and toasts ("export at 60%") via SSE.

Not for: multi-step processes with state between steps (that is
`dagnats`); timers bound to one specific entity (no owner here).

### `database` — app data in PocketBase/SQLite (core)

What: collections, seeds, and the admin UI at `/_/`.

- Todos, users, credits ledger, any CRUD the product owns.

Not for: realtime fan-out (that is `nats` + SSE or PB realtime);
offline-first conflict resolution (that is `offline-sync` + `collab`).

### `auth` — cookie sessions + middleware (core)

What: login flow, session cookies, route gating. The middleware stays;
the login page itself is removable UI.

- Any product with accounts, per-user data scoping, auth-gated pages.

Not for: API-token auth for machines (not included); BYOK accounting
(that is `credits`).

### Plumbing (core, not a decision)

`server` (shared boot), `secrets` (age env loader), `routeutil`
(per-method routes), `app` (deps bundle). You never keep-or-drop these;
skip this section when deciding.

## Plugins

### `nats` — cross-process messaging (plugin, env-blessed)

What: embedded NATS JetStream — broadcast, KV, streams, leaf nodes.
Runtime off: `NATS_ENABLED=false` (in-memory hub fallback).

- Second web instance behind a load balancer sharing live updates.
- Desktop edge syncing offline edits to the server (leaf nodes).
- Durable streams any consumer can replay (`app.sync.*`, `app.crud.*`).

Not for: single-process deployments (the hub covers fan-out; see
"Topology profiles" below). Deletion is manual (15+ importers carry
NATS types) — toggle, don't trim.

### `dagnats` — durable multi-step workflows (plugin, offered)

What: DAG engine over JetStream: retries, timeouts, signals
(suspend/resume), cron/webhook/NATS/HTTP triggers, DLQ with replay,
operator console at `/dagnats/`. Runtime off: `DAGNATS_ENABLED=false`.

- Onboarding drip that pauses until the user's first todo, then resumes.
- Nightly pipeline: fetch → transform → notify, each step retried.
- Approval flows that wait days for a human signal.
- Agent pipelines (plan → code → test → review loops).

Not for: single fire-and-forget jobs (that is `queue`); per-step undo
on failure — DagNats retries and replays, it does not compensate
(see saga note below); timers owned by one entity (no owner here).

### `goakt` — entity actors with supervision (plugin, offered)

What: one addressable grain per room (heartbeat roster plus an
exactly-one presenter lock), supervised with restart budget, idle
passivation. Standalone: no network, no discovery, no cluster.
Runtime off: `GOAKT_ENABLED=false`.

- Presenter/turn arbitration exactly one can win (KV stores and
  pub/sub cannot arbitrate — concurrent acquirers race there).
- Per-entity timers and heartbeat-driven membership that rebuilds
  after a restart (soft state, never persisted).
- Supervised workers that must come back after a panic, with a
  retry budget instead of unbounded restarts.

Not for: converging data across replicas (that is `collab`/Loro);
fire-and-forget jobs (that is `queue`); multi-step DAGs with replay
(that is `dagnats`); cross-process placement (standalone only —
cluster mode is out of scope for this unit).

### `llm` — model calls (plugin, env-blessed)

What: GoAI client behind an injectable interface (any OpenAI-compatible
provider), streaming, function-field injection for tests.
Runtime off: unset `GOAI_API_KEY` (suggest button hides).

- "Suggest a better title" buttons, streaming completions, BYOK
  metering via `credits`.

Not for: orchestration, memory, tool loops, supervision — single
calls only. Multi-step agent behavior is `dagnats` (pipelines), not this.

### `collab` — realtime convergence (plugin, bundled with whiteboard)

What: Loro CRDT DocStore, sync workers, presence transport.
Never standalone (needs a producing feature: canvas, notes).

- Two cursors drawing at once, offline edits that merge without a
  server round-trip.

Not for: ownership or turn-taking (CRDTs converge data; they don't
elect owners — no locks, no presenter token).

### `credits` — AI metering + BYOK ledger (plugin, offered)

What: per-key usage accounting for metered LLM features.
Runtime off: `CREDITS_ENABLED=false` (default; unmetered but working).

- Pay-per-use AI features, customer API keys with quotas.

Not for: the model call itself (that is `llm`); subscriptions and
invoicing (not included).

### `entity-store` — where entities live (plugin, runtime switch)

What: `ENTITY_STORE=pb|crdt` — PocketBase rows (default, server
authority) vs Loro CRDT docs (converge without a round-trip).

- `pb`: accounts, billing, anything needing server truth.
- `crdt`: canvases and docs edited offline on flaky networks.

Not for: both at once per entity — one strategy per store.

### `offline-sync` — offline edits that arrive later (plugin, manual removal)

What: service-worker outbox + NATS CRUD proxy + idempotency.
Runtime off: `OFFLINE_SYNC_ENABLED=false`.

- Field techs selling with no signal; edits replay on reconnect.
- Desktop edge queuing CRUD while disconnected (leaf nodes).

Not for: conflict *resolution* on merge (that is `collab`); pure
read-only offline pages (a cached shell needs none of this).

### `sounds`, `skins`, `datastar`, `components` (plugins)

Small surface: UI sound cues (`sounds`, offered), non-default skins
(`skins-extra`, offered), rendering helpers and shared UI plumbing
(`datastar`, `components` — manual removal). Audible feedback and
theming only; no business logic depends on them.

## Features (user surfaces)

### `todo` — reference CRUD (feature, manual removal)

What: the MVC demo every pattern in this repo points at. Keep it as
the implementation reference; delete it manually when done reading.

### Decision pairs (same need, different unit — pick once)

- **One job vs a workflow:** fire-and-forget with retry ("send this
  email", "resize that upload") → `queue`. Steps with state between
  them, waits, or fan-out ("onboard, then pause for first todo, then
  create three examples") → `dagnats`.
- **Live update plumbing:** same-process browser push → SSE hub (via
  `queue`). Cross-instance or desktop-edge delivery → `nats`. Merging
  offline edits without a server round-trip → `collab` (+`offline-sync`
  for the outbox).
- **Where entities live:** server truth, billing, accounts → `entity-store`
  `pb`. Shared canvas/docs edited offline → `crdt`.
- **Public vs operator surface:** unauthenticated homepage → `landing`;
  auth-gated env inspection → `config-view`.

### No unit for these (yet) — advise answers the full map

Real needs with no registry entry, verified against `advise` (no preset
matched — decide from the table or build custom; do not force-fit):

- Online store (menu, orders, pickup, checkout) — menu display alone is
  `landing`; orders/payments have no unit.
- Reservations and booking (clinic slots, restaurant tables) — the admin
  side is `config-view`; booking itself has no unit.
- Support chat and chatbots — `llm` makes the call, but conversation
  memory, routing, and supervision have no unit.
- Photo galleries, donations, volunteer rotas — static display is
  `landing`; the workflows behind them have no unit.
- Per-entity timers and single-owner locks (one cart, one room owner) —
  presenter locks are `goakt`; per-entity timers and conversation
  supervision remain evaluation triggers (no unit yet).

### `whiteboard` — shared canvas (feature, offered)

What: Loro canvas + Rough.js + presence (pulls in `collab`).

- Brainstorm boards, seating charts, any shared drawing surface.

Not for: owner arbitration (no presenter lock); canvas and notes share
`collab` today.

### `room` — presence demo page (feature, offered, bundled with `goakt`)

What: the `/room/` page exercising the `goakt` engine (roster,
presenter lock, crash hook). A reference implementation like `todo`,
for entity actors.

- Seeing supervised restart recovery live; the pattern to copy for
  per-entity features.

Not for: production rooms (no persistence, single process) — copy
the grain pattern into your own feature instead.

### `notes` — shared plain text (feature, manual removal, rides the whiteboard unit for trim)

What: the `/notes/` pages exercising server-owned Loro Text (character
ops merge, resolved text streams to peers). A reference implementation
like `todo`, for collaborative editing without a JS CRDT library.

- Meeting notes, shared scratchpads, any plain-text surface where
  concurrent typing must never lose characters.

Not for: rich text (no marks, no cursors shared); offline-first editing
(ops need the server — a dropped connection holds text in the tab only).

### `landing` — public marketing page (feature, offered)

What: unauthenticated `GET /`, brand home. Dropping it 404s `/`
(brand retargets to `/todo`).

- Homepage, hero, public product surface.

### `config-view` — ops visibility (feature, offered)

What: auth-gated read-only `/config` (env view for operators).

- Dashboards for admins, environment inspection without SSH.

## Topology profiles (when the stack shape changes)

One process (web binary only, no desktop, no second instance) never
needs cross-process transport. That profile runs today via runtime
switches, no rebuild: `NATS_ENABLED=false` (hub fallback),
`DAGNATS_ENABLED=false` (workflows no-op), don't ship desktop.
Degradations to know: onboarding pauses (DagNats no-op — rewrite as
`queue` job chains for full parity), whiteboard sync and presence KV
expect NATS (verify before relying on them switched off).

Multi-process (desktop edges, second instance, external workers)
is the connected profile: NATS on, leaf nodes, crud proxy —
the default you scaffolded. The cut is exactly this: **does any
second process need shared state?** No → single profile. Yes →
connected profile. A hard installer variant (clean single-process
tree, no dead code) does not exist yet; the switches above are the
blessed path until one is demanded.
