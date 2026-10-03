# Six async layers

Most templates force you to pick **one** async strategy — usually a queue,
sometimes a workflow runtime, rarely both. As your app grows you will hit
problems that **each** of these solves: a **queue** for background jobs, a
**workflow runtime** for durable multi-step processes, a **collaboration
layer** for conflict-free state merging, and a **real-time layer** for
cross-client state.

This template ships all six in one unified build. Use what you need; the rest
sits dormant until you don't.

```
goqite       → background jobs + SSE hub (always on)
dagnats      → durable multi-step workflows as JSON (opt-out: DAGNATS_ENABLED=false)
Loro CRDT    → collaborative docs with offline merges (remove internal/collab/)
PB realtime  → record-change push via PB's native /api/realtime (always on, per-user scoped)
SSE Hub      → ephemeral signals via Datastar protocol (always on, part of queue)
JetStream    → multi-instance broadcast + cross-instance state (opt-out: NATS_ENABLED=false)
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

The whiteboard avoids this entirely: Loro CRDT ops already carry unique IDs and
converge idempotently on their own.

## The opt-out rules

**Infrastructure components** (NATS, DagNats) have runtime env vars in
`config/config.go`. Set `NATS_ENABLED=false` or `DAGNATS_ENABLED=false` and the
engine won't boot; downstream consumers handle nil gracefully.

**Product features** (Todo, Whiteboard) have no runtime flag. To remove them,
delete the package directory and remove the wiring call from
`router/router.go` — that is the [SCOPE removal pattern](scope-taxonomy.md).

They coexist in the same binary. They do not compete.

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