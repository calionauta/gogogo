# Seven async layers

Most templates force you to pick **one** async strategy — usually a queue,
sometimes a workflow runtime, rarely both. As your app grows you will hit
problems that **each** of these solves: a **queue** for background jobs, a
**workflow runtime** for durable multi-step processes, a **collaboration
layer** for conflict-free state merging, a **real-time layer** for
cross-client state, and an **entity layer** when exactly one owner must
decide (a lock, a turn, a roster).

This template ships all seven in one unified build. Use what you need; the rest
sits dormant until you don't.

```
goqite       → background jobs + SSE Hub delivery (always on)
dagnats      → durable multi-step workflows as JSON (opt-out: DAGNATS_ENABLED=false)
Loro CRDT    → collaborative docs with offline merges (remove internal/collab/)
SSE record   → record-change push, visibility-scoped (always on)
SSE ephemeral→ UI signals via Datastar protocol (always on, part of queue)
JetStream    → multi-instance broadcast + cross-instance state (opt-out: NATS_ENABLED=false)
GoAkt grains → one addressable owner per room: roster, locks, supervision (opt-out: GOAKT_ENABLED=false)
```

Jobs run once: the queue visibility window (10min) exceeds the slowest
legitimate job, so a slow LLM call is never re-executed mid-flight
(goqite redelivers past the window, up to MaxReceive — persistence
dedupes by idem key, live SSE sends do not, so keep handlers fast or
the window long).

## Transport matrix (queryable)

Which wire carries what, what it falls back to, and the switch. There is
deliberately no single transport switch: records need visibility scoping
(private rows to the owner's tabs, shared rows to every login — free with
the owner + visibility columns), ephemeral traffic needs fan-out without
persistence — one knob would conflate both. Pick by traffic type:

| Traffic | Mechanism | Default | Switch / fallback | Notes |
|---|---|---|---|---|
| Record mutations (CRUD) | SSE Hub record event + fragment re-fetch | always on | none (Hub-native; scoping comes from the owner + visibility columns) | the todo list resyncs its fragment on it; the notes/whiteboard INDEXES have no stream and refetch on load + tab visibility (`ListResync`), so a doc created in another tab appears when that tab is focused again (their per-doc streams carry the text/shapes themselves) |
| Ephemeral signals (toasts, progress, presence) | SSE Hub (Datastar SSE) | always on | in-process only; no cross-instance without JetStream | `BroadcastExcept` excludes the originator |
| Cross-instance broadcast + CRUD proxy | NATS JetStream | on | `NATS_ENABLED=false` → in-memory hub; whiteboard/notes ops go SSE-only single-instance | `realtimeLabel()` surfaces the active transport in the UI |
| Doc convergence (offline merges) | Loro CRDT + NATS subjects | on with collab | workers no-op when NATS is down | snapshots persist to Turso tables |
| AI streams (tokens, run framing) | SSE Hub + AG-UI envelope | on with llm | unset `GOAI_API_KEY` hides the UI; envelope in `features/genui/` | byte-compatible with AG-UI clients, no new transport |
| Stale-tab detection | `X-Gogogo-Build` on `/health` | on | none; inert in dev (`dev/` tag never changes) | single-binary deploys only (see troubleshooting) |

The same matrix is queryable at runtime: `advise --need "broadcast transport"` (preset `realtime-transport`).

## Two realtime mechanisms for different jobs

This is the distinction that matters most, and the one most templates get
wrong.

**The SSE Hub record event** pushes *record mutations*
(create/toggle/delete) to subscribers, scoped by row visibility (private
rows to the owner's tabs, shared rows to every login's streams). It is the
mechanism for **database actions**.

**The SSE Hub ephemeral channel** (`internal/queue/ssehub.go`) carries
**ephemeral signals**: client count, LLM suggest feedback, workflow
progress, and the originating client's own synchronous patch. It delivers
them via Datastar's SSE protocol (`internal/datastar.RenderAndPatch` /
`MergeSignals`).

The todo feature uses both: the record event for CRUD propagation, ephemeral
signals for toasts and live hints. The whiteboard uses the SSE Hub directly
for shape and presence broadcast.

**Ephemeral draft channel (a reusable pattern).** The whiteboard's live
drawing is the reference for "show work in progress without committing it".
While a shape is being drawn, every pointermove posts one fused frame on the
same volatile presence relay (`/presence`) — normalized cursor plus the whole
in-progress shape (`type:"draft"`) — never persisted, never
merged into the CRDT, never published to NATS — and peers repaint dot and
corner from the same instant, so they cannot skew apart. Frames carry the
sender-captured `cts` and receivers drop anything older than the last applied
frame per user, so a retried POST can never drag state back in time.
The real `add` op on pointer-up remains the only commit, and a
peer drops the draft the instant the committed shape with that id arrives
(plus a TTL backstop). Each frame carries the **whole** shape, not a delta,
so a dropped frame is healed by the next one and the channel needs no
sequencing or acks. Reuse it for anything whose *finished* state belongs in
the durable store but whose *in-progress* state only needs to be seen:
dragging an existing shape, a resize handle, a live text selection.
The rule of thumb: **commit on release, relay while moving.**

> **Do not add a parallel SSE-hub re-render for record mutations.** If a record
> changed, the Hub record event already delivers it, and the visibility-scoped
> re-list makes the delivery correctly scoped for free.

## Cross-instance sync adds one more path

When JetStream is enabled (default: on):

- The **NATS APP_CRUD stream** converges record operations across server
  instances. `CrudPublisher` publishes each mutation tagged with the
  publisher's **instance identity** (`APP_INSTANCE_ID`, else the hostname);
  `CrudConsumer` runs **one durable per instance** (`<app>-crud-<instance>`,
  inactive for an hour after a rollout) so every instance applies every
  remote op to its own Turso database and broadcasts it once to its local
  clients. The publishing instance skips its own event — the local write and
  fan-out already happened — which is what keeps a mutation from producing
  two record events. A durable shared by two instances would be rejected by
  the server ("consumer is already bound"), and the losing instance would
  stop converging silently.
- Inside the feature, **one publish point** (`features/todo/bus.go`) fans the
  fact out to the hub, the wire, and any in-process subscriber (a native
  window). Handlers call it once; no mutation can reach one transport and
  miss the other.

The stream is safe to enable on a single instance — it simply carries no
cross-instance traffic.

## Offline sync uses yet another path

**On the web**, the Service Worker (`web/resources/static/sw.js`) intercepts
POST/PUT/DELETE mutations when the browser is offline, queues them in
IndexedDB, and replays them via Background Sync when connectivity returns.

**On the desktop**, the NATS Leaf Node keeps its local JetStream replica in
sync; the local JetStream persists mutations to disk and replays them when the
Leaf Node reconnects to the server. No Service Worker needed.

### Replay is dedup'd at the server

The todo create form attaches a fresh `idem_key` UUID to every submit.
The create path looks up `(idem_key, owner)` and returns the existing
row on a match, so a Service Worker replay does not create a duplicate
todo.

The whiteboard needs no idempotency key for a different reason: a shape op is
keyed by the shape's own id inside a LoroMap, so re-applying the same op resolves
to the same state rather than creating a second row. Replay is therefore
state-idempotent without a `(idem_key, owner)` match — measured, not assumed (see
the CRDT concurrency tests in `internal/collab/`).

What the map's last-writer-wins semantics did NOT give is a signal when two
writers edited the **same** shape: every replica agreed deterministically on
which write survived, but the loser was dropped silently. Shapes therefore carry
a server-assigned `Version`, and a client sends back the revision it last saw as
`ShapeOp.BaseVersion`. A write based on a superseded revision is refused with
**409 + the current shape list**, so the browser resyncs and the edit can be
re-applied instead of vanishing. A rejected op is neither persisted nor
broadcast. `clear` takes no version because it cannot conflict.

Note the scope: the canvas today only *adds* shapes, so this was not yet
reachable from the UI — it becomes load-bearing as soon as shapes can be moved or
resized.

## Entity-addressed messaging owns what converging cannot

Broadcast converges data; it never elects an owner. When two browsers grab
the same presenter lock, a KV put races and pub/sub duplicates — the grain
serializes: one room, one mailbox, exactly one winner, by construction.

A grain (`internal/goakt/`) holds a heartbeat roster plus the lock,
supervised with a restart budget and passivated when idle. Roster state is
soft (rebuilt from heartbeats, never persisted — see the durability table
below). The `/room/` demo exercises the whole loop including a crash hook;
copy the grain pattern for per-entity features. Roster reads over HTTP are
retried within a bounded budget, so the supervised-restart window answers a
moment later instead of with a 502; the grain error is logged, never
returned in the response body. Standalone only: no
placement, no cluster — cross-process entities are out of scope.

## The opt-out rules

**Infrastructure components** (NATS, DagNats) have runtime env vars in
`config/config.go`. Set `NATS_ENABLED=false` or `DAGNATS_ENABLED=false` and the
engine won't boot; downstream consumers handle nil gracefully.

**Product features** (Todo, Whiteboard, Room, Notes) have no runtime flag. To remove them,
delete the package directory and remove the wiring call from
`internal/server/chi.go` — that is the [SCOPE removal pattern](scope-taxonomy.md).

They coexist in the same binary. They do not compete.

## Durability and retention (per store)

Every store below is crash-recoverable by design (SQLite WAL replay,
JetStream recovery, goqite), so backup means files, not coordination.
The invariant to protect: **no truth lives in two places** — snapshots
derive one way (Loro → Turso), outboxes drain one way, grain rosters
rebuild from heartbeats.

| Store | Retention | Backup |
|---|---|---|
| `data/app.db` (Turso app truth) | relational, kept | `scripts/backup.sh` (`.backup` + integrity check) |
| `data/queue.db` (jobs) | jobs until acked + retried | same script, same check |
| APP_CRUD stream (transport buffer) | 7d + 512MB cap (the database is the truth; replaying older would resurrect applied mutations) | same as above |
| DagNats streams/KV (upstream) | engine-owned; 10 GiB store cap + 30d DLQ | same as above |
| Browser outbox (IndexedDB) | device-local, drains on reconnect | not backed up (client state) |
| Grain roster (GoAkt) | in-memory soft state, heartbeat-rebuilt | not backed up (rebuilt, by design) |

Restore drill (monthly, cheap): `./scripts/restore.sh --dry-run <backup>`
— an untested backup is a rumor. Full procedure lives in
[deploy](deploy.md#backup-and-restore).

## One `make build` compiles everything

No build tags, no stub files, no matrix. DagNats and NATS share a **single
embedded JetStream on `:4222`** — DagNats boots it and the whiteboard SyncWorker
attaches to it, so there is only one NATS process in the binary.

## Offline-first is baked in, not bolted on

Web clients intercept mutations in a Service Worker and replay them via
Background Sync; the desktop build becomes a NATS Leaf Node that keeps its
JetStream replica in sync while offline. Replays are deduplicated server-side
(the todo create form attaches an `idem_key` that the idempotency hook
collapses).

Set `OFFLINE_SYNC_ENABLED=false` for always-online deployments and **zero code
paths are traversed** — the flag disables the NATS CRUD proxy and the Service
Worker offline queue together.

## Related

- [Stack in layers, not silos](stack-layers.md)
- [Features](features.md)
- [Configuration](configuration.md)