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
PB realtime  → record-change push via PB's native /api/realtime (always on, per-user scoped)
SSE Hub      → ephemeral signals via Datastar protocol (always on, part of queue)
JetStream    → multi-instance broadcast + cross-instance state (opt-out: NATS_ENABLED=false)
GoAkt grains → one addressable owner per room: roster, locks, supervision (opt-out: GOAKT_ENABLED=false)
```

## Two realtime mechanisms for different jobs

This is the distinction that matters most, and the one most templates get
wrong.

**PocketBase's native `/api/realtime`** pushes *record mutations*
(create/toggle/delete) to subscribers, scoped per-user by the collection's
access rules. It is the mechanism for **database actions**.

**The SSE Hub** (`internal/queue/ssehub.go`) is reserved for **ephemeral
signals**: client count, LLM suggest feedback, workflow progress, and the
originating client's own synchronous patch. It delivers them via Datastar's
SSE protocol (`internal/datastar.RenderAndPatch` / `MergeSignals`).

The todo feature uses both: PB realtime for CRUD propagation, SSE Hub for
toasts and live hints. The whiteboard uses the SSE Hub directly for shape and
presence broadcast.

> **Do not add a parallel SSE-hub re-render for record mutations.** If a record
> changed, PB realtime already delivers it, and the collection's `ListRule` /
> `ViewRule` (`@request.auth.id != '' && owner = @request.auth.id`) make the
> delivery per-user scoped for free.

## Cross-instance sync adds two more paths

When JetStream is enabled (default: on):

- The **NATS APP_CRUD stream** converges record operations across server
  instances. `CrudPublisher` publishes each mutation; `CrudConsumer` on the
  receiving instance writes to its local PocketBase, which then broadcasts via
  PB realtime to its local clients.
- The **NATS TODOS stream** carries ephemeral signals across instances and
  re-emits them through the local SSE Hub.

Both are safe to enable on a single instance — the streams simply carry no
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
`db/idempotency_hook.go` intercepts `OnRecordCreateRequest` and returns the
existing record on a `(idem_key, owner)` match, so a Service Worker replay does
not create a duplicate todo.

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
copy the grain pattern for per-entity features. Standalone only: no
placement, no cluster — cross-process entities are out of scope.

## The opt-out rules

**Infrastructure components** (NATS, DagNats) have runtime env vars in
`config/config.go`. Set `NATS_ENABLED=false` or `DAGNATS_ENABLED=false` and the
engine won't boot; downstream consumers handle nil gracefully.

**Product features** (Todo, Whiteboard, Room, Notes) have no runtime flag. To remove them,
delete the package directory and remove the wiring call from
`router/router.go` — that is the [SCOPE removal pattern](scope-taxonomy.md).

They coexist in the same binary. They do not compete.

## Durability and retention (per store)

Every store below is crash-recoverable by design (SQLite WAL replay,
JetStream recovery, goqite), so backup means files, not coordination.
The invariant to protect: **no truth lives in two places** — snapshots
derive one way (Loro → PB), outboxes drain one way, grain rosters
rebuild from heartbeats.

| Store | Retention | Backup |
|---|---|---|
| `data.db` (PB app truth) | relational, kept | `scripts/backup.sh` (`.backup` + integrity check) |
| `data/queue.db` (jobs) | jobs until acked + retried | same script, same check |
| TODOS stream (live fan-out) | 24h + 256MB cap (consumers are live-only; catch-up is PB + fragment re-fetch) | `nats account backup` when reachable (ephemeral by design — losable) |
| APP_CRUD stream (transport buffer) | 7d + 512MB cap (PB is the truth; replaying older would resurrect applied mutations) | same as above |
| `room-presence` KV (roster) | latest revision per member + 24h TTL (crashed members expire) | same as above |
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