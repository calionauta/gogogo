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
SSE ephemeral→ UI signals via Datastar protocol (always on; core-NATS bridge
               for cross-instance when enabled)
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
| Record mutations (CRUD) | SSE Hub record event + fragment re-fetch | always on | none (Hub-native; scoping comes from the owner + visibility columns) | the todo list resyncs its fragment on it; the notes/whiteboard INDEXES have no stream and refetch on load + tab visibility (`ListResync`), so a doc created in another tab appears when that tab is focused again (their per-doc streams carry the text/shapes themselves). Non-browser clients use `/api/todos/events` (JSON frames from the same event bus, same owner/visibility scope) instead of parsing Datastar |
| Ephemeral signals (chat answers, toasts, workflow progress) | SSE Hub (Datastar SSE) + core-NATS bridge | always on; bridge on with NATS | NATS off → in-process only; a failed publish falls back to local delivery | only `Send`/`Broadcast` ride `app.ephemeral`; record and doc fan-out keep their own wires so nothing is delivered twice. Best-effort (core NATS, no replay) |
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

> **An ack must never write a signal the worker's terminal patch owns.** A POST
> that enqueues a job answers on its own response, while the result arrives on
> the tab's SSE stream — two connections, no ordering guarantee between them, and
> the response is the slower one whenever the caller is behind a tunnel or
> Datastar retries it. The suggest ack merged `aiPending: true` and
> `suggestions: []`, so it could land *after* the result: all three steps lit,
> the button stuck on "Asking AI…", the suggestions erased. The ack now merges
> nothing — the click owns the optimistic state, the worker owns the terminal
> state — and the enqueue failure path patches that terminal state onto the
> already-open stream, because a 500 has nothing left to clear the spinner with.
>
> **This is a rule for EVERY enqueueing endpoint, not a per-endpoint fix.** The
> same shape was found in four places, and the difference is only how bad the
> late ack is:
>
> | Endpoint | Signal the ack used to own | Symptom of a late ack |
> |---|---|---|
> | `POST /api/todos/suggest` | `aiPending`, `suggestions` | spinner stuck on "Asking AI…", results erased |
> | `POST /api/todos/retry-demo` | `lastRetry` | the last attempt's text overwritten by "queued" |
> | `POST /api/chat/ask` | `chat_thinking` | **the Send button stays disabled forever** |
> 
>
> The last two are worse than a stuck spinner: their button is
> `data-attr:disabled="!!$chat_thinking"`, so a re-asserted `true` is a DEAD
> control — the composer can never be used again without a reload. The rule
> generalises: an ack that enqueues a job writes no state signal at all; the
> client owns the optimistic state, the worker owns the terminal state, and the
> failure path patches the terminal state onto the open stream.

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
- The **ephemeral bridge** (`internal/queue/ssehub_ephemeral.go`) carries the
  hub's `Send`/`Broadcast` signals — a chat answer produced on one instance
  but consumed on another, onboarding progress, suggest/retry toasts — over
  core NATS on `app.ephemeral`. Producers publish once; every instance,
  including the publisher's, delivers through its local hub (one path, so no
  origin tag is needed). It is deliberately limited to those two methods:
  record events and doc updates already have wires of their own. A publish
  failure falls back to local delivery so this instance's tabs never lose a
  signal; core NATS is best-effort, so a down instance can miss one (the same
  trade-off as any ephemeral channel).

The stream is safe to enable on a single instance — it simply carries no
cross-instance traffic.

## Offline sync uses yet another path

**On the web**, the Service Worker (`web/resources/static/sw.js`) intercepts
POST/PUT/DELETE mutations when the browser is offline, queues them in
IndexedDB, and replays them via Background Sync when connectivity returns.

A replay is NOT single-shot. The window's `online` event fires when the OS
reports a link, not when packets move, so the first replay fetch can fail a
few hundred milliseconds before the network is usable (measured live
against the production deploy). The queue therefore retries each item with
bounded backoff (`REPLAY_BACKOFF_MS`), re-registers Background Sync while a
transient failure remains, and the page keeps asking for a drain while its
own `queueCount` (a SW round-trip) still sees rows (bounded `KICK_DELAYS`). Until the SW
reports `sync-end`, the offline banner says "Changes queued — retrying
sync…" instead of claiming "Online": a pending mutation must never look
saved.

Each outcome has its own policy, not a generic retry:

- **2xx / 404** — settled (404 means the row is already gone).
- **network, 5xx, 429, 408** — transient: keep the item, stop the PASS (every
  later item shares the condition), and register Background Sync. A
  `Retry-After` up to 5s is honoured before the retry; a longer one is
  Background Sync's job.
- **401** — the session is gone: keep the item, pause the queue, stop both
  retry layers, and show "Changes queued — sign in to sync" with a link back
  to the page. Retrying an expired session is a loop, not a retry. The shell
  also recovers a 401 from any scripted action by sending the user to
  `/login?next=…` (a page load is bounced server-side).
- **other 4xx** — permanent for this body: drop it so one invalid mutation
  cannot wedge every later one, and tell the user (toast), not just the
  console.

The FIRST submit follows the same policy, not only a replay. The worker used
to queue only when `fetch` threw, so an app that was reachable on the wire but
down behind the gateway (Cloudflare answers 520/521/522 for an unreachable
origin, 502 for a dead upstream, 503/504 for an overloaded edge) came back as a
failed action — "Not saved", the title kept in the box, nothing queued. Only a
DEFINITIVE answer (2xx/3xx/other 4xx) may settle an action; a transient status
is queued exactly like a dropped connection. This is scoped to the routes the
queue owns (`QUEUE_OWNED_PREFIXES`, the todo CRUD): a 5xx from a chat turn or a
sign-in is an answer, and re-sending it later would duplicate a message or
report a false success.

`KICK_DELAYS` is bounded, so a queue that outlives it needs a slower owner. The
page keeps a drain cadence (`DRAIN_DELAYS`, 5s growing to 30s) while
`queuedPending` is set. It exists because the outage can be the APP while
`navigator.onLine` stays true — a gateway 5xx, an origin restart, a deploy — and
then neither the `online` event nor Background Sync is guaranteed to fire
again; without it the row sat in IndexedDB until the next reload.

The queue is a **per-account** outbox. Each entry carries the owner key the
page computed (SHA-256 of the signed-in email) and a `queuedAt` stamp; a
replay only touches entries for the currently signed-in owner and drops
entries older than `MAX_QUEUE_AGE_MS` (one week) or beyond
`MAX_QUEUE_ITEMS` (100). Dropped entries (foreign account, stale, rejected,
or evicted from a full queue) surface as a toast: a change that will never be
saved must say so. Sign-out asks before discarding queued changes and then
posts `clear-user-data`, which clears both the page cache and the queue —
otherwise the next person on a shared device would inherit them.

The page asks the SW for the queue count (`queue-status` message) instead
of opening the queue database: the schema then has one owner. A second
opener without the SW's upgrade handler once left a v1 database with no
`pending` store, which silently lost every queued mutation.

The SW knows nothing about feature markup: it reports the queued request and
answers `204`, and the FEATURE renders its optimistic state. For todo that is
`web/resources/static/todo-offline.js` (row, toggle, delete, clear); a second
feature registers its own listener for `gogogo:queued` instead of editing the
worker. The placeholder is inserted where the server list would put it — the
list is newest first (`created DESC, id DESC`) — so the replay's re-render
keeps the row in its slot; appending it made the same todo jump from last to
first once the server answered. It also carries the row contract the server
expresses as separate queries (`data-created` plus a `data-show` guard): a todo
created now is active, owned by you and absent from any search result, so the
placeholder hides under the completed tab, `scope=shared` and an active search
instead of floating above a list it does not belong to.

A queue write that cannot complete is NOT a success. The worker bounds the
IndexedDB OPEN with `IDB_OPEN_TIMEOUT_MS` (and gives an `onblocked` upgrade a
short grace before failing) — a blocked upgrade or disabled storage can open
neither success nor error — answers `503` when it fails, and announces the loss
(`queue-lost`, reason `storage`). A `503` means nothing was written: it comes
from a failed/blocked open or an aborted transaction, never from a write that
may still land, so a client retry cannot duplicate it. Answering `204` anyway
told the page "queued" while nothing was stored: the Add button stayed in its
loading state forever and the todo was gone. On the page side the pending state
belongs to the REQUEST: the create action releases its spinner when the action
settles, even if the SW message never arrives (the message drives the
optimistic row, not the form), and a failed request keeps the typed title plus
an inline "not saved" alert.

**Online-only mode** (`OFFLINE_SYNC_ENABLED=false`) retires a worker an
earlier deployment installed. The flag only stops the registration; an
existing registration keeps intercepting `/api/*` and answering `204` for a
page that renders neither the bridge nor the optimistic renderer, which is the
same stuck-spinner failure. `offline-banner.js` unregisters it and clears the
caches it owns.

The banner states only what is true. `offline-banner.js` says "changes are
queued" ONLY when a Service Worker controls the page: with no controller
(worker blocked, registration evicted, a browser that refused it) it says the
queue is not active and asks for a reload, because a write made now fails. It
also compares the policy version it loaded with the one the worker imported at
install time (`POLICY_VERSION`, the file's own fingerprint, pinned by
`TestOfflinePolicyVersionMatchesItsDigest`): a mismatch means the browser runs
a worker from an older deploy, and the banner says "out of date — reload"
instead of promising a queue the worker may not implement.

**On the desktop**, the NATS Leaf Node keeps its local JetStream replica in
sync; the local JetStream persists mutations to disk and replays them when the
Leaf Node reconnects to the server. No Service Worker needed.

Notes and the whiteboard keep their OWN outboxes on the page
(`features/notes/static/notes.js` in `localStorage`,
`features/whiteboard/static/whiteboard.js` in IndexedDB): their endpoints are
excluded from the worker because an op stream carries its own convergence
rules. They follow the same transient/permanent split, from the same file the
worker imports (`web/resources/static/offline-policy.js`): a 5xx/429/408 keeps
the batch and retries, an auth 4xx keeps it but stops hammering, and any other
4xx is a verdict, so it is dropped instead of wedging every later op. The
notes client used to treat a gateway 5xx as an invalid op and drop the batch,
so the server never received text typed while the app was unreachable behind
the edge.

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

A grain (`internal/goakt/`) holds a connection-scoped roster plus the lock,
supervised with a restart budget and passivated when idle. Roster state is
soft (rebuilt from connections, never persisted — see the durability table
below). The `/room/` demo exercises the whole loop including a crash hook;
copy the grain pattern for per-entity features. Roster reads over HTTP are
retried within a bounded budget, so the supervised-restart window answers a
moment later instead of with a 502; the grain error is logged, never
returned in the response body.

Presence is the **SSE connection**, not a timer: the stream joins on
connect, leaves on disconnect, and the grain refcounts per-tab client IDs,
so two tabs of one person are one member and closing one does not evict
them. Every mutation re-reads the grain and pushes the whole snapshot to
the room's tabs (a dropped frame is healed by the next), and a tab joining
gets a snapshot immediately — nothing polls. The page keeps its stream while
hidden (`openWhenHidden`), because a dropped connection IS a departure; and a
crash restart replays the live clientIDs into the fresh grain, so "watch the
roster rebuild" rebuilds from connections instead of from timers. `POST /room/heartbeat` still
exists for non-browser clients and folds one synthetic connection per name.

Two properties the demo pins because both were wrong: the roster is
**sorted by name** (ranging a Go map randomized the order between pushes,
so the list flickered), and re-acquiring a lock you already hold
**succeeds** (a holder told "no" sees a dead button). Leave drops the
connection and, when it was the last one, the member and any lock they
held — and it sticks, because no timer re-adds them. Leaving is a state the
tab writes, not just a silent roster edit: the leave answer flips a
per-viewer `$roomLeft` signal, so the leaver's own card says so, hides the
roster actions and offers **Rejoin** (`POST /room/join`). That flag rides no
broadcast — a shared push cannot know who is reading, and one that shipped
`roomLeft:false` would hand the buttons back to the tab that just left. Two
paths clear it, both because presence IS the connection: a rejoin, and the
tab's own stream reconnecting (`online`/`pageshow` re-open it and the server
sees a new connection). A crash rebuild does the same for every live stream,
which is why Crash is hidden while left.
Standalone only: no placement, no cluster — cross-process entities are out of
scope.

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
rebuild from connections.

| Store | Retention | Backup |
|---|---|---|
| `data/app.db` (Turso app truth) | relational, kept | `scripts/backup.sh` (`.backup` + integrity check) |
| `data/queue.db` (jobs) | jobs until acked + retried | same script, same check |
| APP_CRUD stream (transport buffer) | 7d + 512MB cap (the database is the truth; replaying older would resurrect applied mutations) | same as above |
| `app.ephemeral` (core NATS, not JetStream) | none — best-effort, losable by design | not backed up |
| DagNats streams/KV (upstream) | engine-owned; 10 GiB store cap + 30d DLQ | same as above |
| Browser outbox (IndexedDB) | device-local, drains on reconnect | not backed up (client state) |
| Grain roster (GoAkt) | in-memory soft state, connection-rebuilt | not backed up (rebuilt, by design) |

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

The SCOPE unit `offline-sync` is a plugin (it can be trimmed); on this async
axis it is the mode above, not one of the seven layers.

## Related

- [Stack in layers, not silos](stack-layers.md)
- [Features](features.md)
- [Configuration](configuration.md)