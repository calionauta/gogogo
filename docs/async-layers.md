# Seven async layers

Most templates force you to pick **one** async strategy — usually a queue,
sometimes a workflow runtime, rarely both. As your app grows you will hit
problems that **each** of these solves: a **queue** for background jobs, a
**workflow runtime** for durable multi-step processes, a **collaboration
layer** for conflict-free state merging, a **real-time layer** for
cross-client state, and an **entity layer** when exactly one owner must
decide (a lock, a turn, a roster).

This template ships all seven in one unified build. Use what you need; the rest
sits dormant until you don't. Delivery (`docs/notifications.md`) is an
application of these, not an eighth layer: inbox rows are records, the delay is
a `sleep` step, and the stream is the SSE hub.

```
goqite       → background jobs + SSE Hub delivery (always on)
dagnats      → durable multi-step workflows as JSON, e.g. onboarding and the
               inbox reminder (opt-out: DAGNATS_ENABLED=false)
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

## Offline writes use the outbox, not the worker

**On the web**, every todo write goes through one durable outbox
(`web/resources/static/offline-outbox.js`) that runs in the PAGE. The intent is
stored before the request leaves, and the same intent is retried until the
server confirms it.

Two stores, on purpose:

- **`localStorage` is the crash journal**, because it is synchronous. `begin()`
  writes the intent and returns its idempotency key in the same tick, so the
  durable copy exists before the request — a tab closed during a hanging request
  cannot lose the write.
- **IndexedDB is the durable, queryable store** the drain reads. The write is
  started immediately and the journal entry is cleared only after it commits; a
  tab that dies mid-commit keeps the journal entry, and `recover()` merges it
  back on the next load.

Both live in the same browser bucket, so an eviction takes them together. The
journal is a crash journal, not a second source of truth.

**A replay is at-least-once, and the server makes it exactly-once.** Every
request carries its key as `Idempotency-Key`, but only a mutation that is NOT
idempotent by construction needs a receipt: `toggle` is a read-then-flip, so
`applyOnce` claims its key in `op_receipts` **inside the same transaction** as the
update, and a second delivery answers without re-running the effect. Delete and
clear-completed are idempotent already (a replay 404s / re-deletes nothing), and
create dedupes on `idem_key`; none of them pays for a receipt row. Before this,
a response lost after the commit made the replay flip the row back.

One drainer runs per origin: Web Locks elects it (`gogogo-outbox-drain`), and a
BroadcastChannel fans the count to the other tabs. Without the lock (an older
browser) two tabs could replay the same entry; the receipt makes that harmless.

The banner (`internal/components/offline_banner.templ`) is declarative: the
outbox publishes `gogogo:outbox` / `gogogo:synced` window events, Datastar turns
them into `$bannerCount` / `$bannerAuth` / `$bannerSyncing` signals, and the
markup derives one state word (`internal/ui.shellSignals` seeds them, because the
account menu's sign-out reads the count from outside the banner subtree). There
is no banner script.

The banner reports TRANSPORT TROUBLE, not work in progress. `begin()` stores the
intent and stays silent: the feature's own request is about to go out, and
announcing "1 queued" painted an amber bar for the length of every healthy
create. Only a failed request (`fail()`) or a recovered queue (load/reconnect)
announces a count. For the same reason "Syncing changes…" waits 500 ms before it
shows: a replay that answers inside that window is noise, a slow one is
information. Every pass closes the state when it ends, so a pass that stops on a
retry cannot leave the bar up.

The drain runs on load, on `online`, on `visibilitychange` to visible, and on a
bounded backoff while intents remain. It does **not** run while every tab is
closed: Background Sync — the only API that could — is Chromium-only (Firefox has
a negative standards position, Safari does not implement it), so it was never a
cross-browser guarantee.

**Local persistence contract.** One rule — a mutation is durable locally before
it is sent; reads are server-rendered (or seeded from the server) — and the store
follows the shape of the data:

| surface | local writes | local reads | convergence | store |
|---|---|---|---|---|
| Todo | pending intent (outbox) + composer draft | last visited page (service-worker page cache) + per-view fragment cache | server re-list; `op_receipts` on the toggle | IndexedDB (ordered queue, unique key, transactions) + localStorage (sync crash journal, draft, view cache) |
| Notes | op batch + full-text draft | the resolved text server-rendered into the cached page | 409 + authoritative text, recompute the tail | localStorage (small single blob, written on every keystroke) |
| Whiteboard | op batch, scoped by board | `data-shapes` seeded by the page (with its render time, shown offline); the stream takes over on connect and re-applies queued ops | 409 + authoritative shape list | IndexedDB (larger JSON, async, no 5 MB cap) |

The split is deliberate: **localStorage is for small, synchronous, single-blob
state** (the crash journal, a note draft, one rendered view) because the write
must not be lost to an async commit; **IndexedDB is for queues** because they
need ordering (`autoIncrement`), a unique key and transactions.

**One drainer per queue, not per tab.** Every local queue is shared by every tab
of the same note, board or list, so the drain takes a Web Lock named after the
resource (`gogogo:notes:<doc>`, `gogogo:whiteboard:<doc>`) before it reads and
sends. Under the lock the STORE is authoritative: a peer tab may already have
delivered what this tab still remembers, so the pass re-reads the queue and stops
when it is empty. The board drain is also sequential (a `clear` must not overtake
the `add` it follows) and deletes each op only AFTER the server accepts it — the
old pass cleared the whole store up front, so a tab that died mid-flight lost the
rest of the queue. Board rows carry the board id: the store is per-origin, and
without it two open boards replayed each other's shapes.

The three drains are deliberately NOT one generic engine. Their units differ in
kind — independent idempotent requests with receipts (todo), ordered shape ops
(whiteboard), a recomputed text diff (notes) — and a shared runner would have to
abstract exactly the part that differs. What must not fork is shared instead, in
`web/resources/static/offline-policy.js`: the retry vocabulary, the cross-tab
lock, and the storage-failure report.

**A view cache, not a read replica.** The todo list is still rendered by the
server, and offline the page shows the LAST render of the view the user asks for
(`web/resources/static/offline-views.js`): the fragment is cached after every
online render, keyed by `filter`/`scope`/`query`/`date`, bounded to 8 views and 7
days, and every restore is labelled "showing this view as it was at HH:MM". The
next server render replaces it, and a control that asks for a view with no cached
fragment refuses instead of moving the tab over a list that did not change. A
restore re-applies the queued intents (create/toggle/delete/clear) first, so the
cached rows agree with the work the server has not seen. The debounced search is
the one control that never restores: it fires after the user stopped typing, and
replacing the list then could erase a row created in between. This is the
"online-first with caching" column of the model below — the cache owns no data
and answers no query, so it cannot merge a write or invent a row.

**Background GETs carry no state.** Every Datastar `@get` on a page with a big
signal set (the todo list) sent the WHOLE signal state as a `datastar=` query
param — the todos array included, in every proxy log. The list, resync, badge and
stream actions pass `{filterSignals: {exclude: /.*/}}`: the URL already carries
filter/scope/query/clientID. List retries stay at the default (idempotent, and a
transient 5xx is what retries are for); the badge is the one action with
`retryMaxCount: 0`, because a viewer can only ever get a 403 there — and the
refresh control is not rendered for a viewer at all (the badge route is `user`+).
A failed action also clears `$loading`, so dropping the badge retries did not
trade a storm for a stuck composer.

**The board is a viewport, not a canvas copy.** Shapes are stored in WORLD
coordinates; the view (pan/zoom) maps them onto each screen. The first
authoritative list fits content that falls outside the viewport — a board drawn
on a laptop used to open as an empty white area on a phone (every shape sat to
the right of a 390 px canvas). Wheel zooms, middle-drag pans, two fingers
pan/zoom, and the toolbar has −/+/Fit. Cursor and draft frames carry world
coordinates too, so the same point means the same place on every screen.

**A queue that cannot be stored says so.** A full quota or a blocked
`localStorage` used to be swallowed by the notes and whiteboard writers: the op
lived in memory, the tab closed, and the change was gone with no signal. Both now
report the failure through the same `gogogo:outbox` event the todo outbox uses
(`reason: "storage"`); the banner names the reason, persists the notice, and
restores it on the next load. Losing the VIEW cache is only a warning: it holds
no user data.

Each outcome has its own policy, not a generic retry:

- **2xx / 404** — settled (404 means the row is already gone).
- **network, 5xx, 429, 408** — transient: keep the intent, stop the PASS (every
  later intent shares the condition), and back off. A `Retry-After` up to 5s is
  honoured before the retry.
- **401 / 403** — the session is gone: keep the intent, pause the queue, and show
  "Changes queued — sign in to sync" with a link back. Retrying an expired
  session is a loop, not a retry.
- **other 4xx** — permanent for this body: drop it so one invalid intent cannot
  wedge every later one, and tell the user (toast), not just the console.

The FIRST submit follows the same policy, not only a replay: the intent is
stored before the request, so a request that never settles leaves a replayable
intent instead of a spinner.

`MAX_QUEUE_AGE_MS` (one week) and `MAX_QUEUE_ITEMS` (100) bound the store: a
queued intent is unsaved work, not an archive. Dropped intents (foreign account,
stale, rejected, or evicted from a full queue) surface as a toast — a change that
will never be saved must say so.

The queue is a **per-account** outbox. Each entry carries the owner key the page
computed (SHA-256 of the signed-in email) and a `queuedAt` stamp; a replay only
touches entries for the currently signed-in owner. Sign-out asks before
discarding queued intents, and the server answers the logout with
`Clear-Site-Data: "cache", "storage"` — one header that drops this origin's page
cache, IndexedDB and the service-worker registration, so the next person on a
shared device inherits nothing.

The worker itself (`web/resources/static/sw.js`) is app-shell cache only: it
caches successful navigations and never touches `/api/*`. That is structural, not
a skip list — no stream, cached GET or mutation can pass through it
(`TestServiceWorkerNeverInterceptsAPI`).

**No-cache mode** (`OFFLINE_SYNC_ENABLED=false`) unregisters a worker an earlier
deployment installed and clears its caches, so a navigation needs the network.
The write path is unchanged: the outbox runs on every page, its intent is
durable, and a replay still lands when the app answers again. The flag is about
the cache, not about losing writes.

### Replay is dedup'd at the server

The todo create form attaches a fresh `idem_key` UUID to every submit.
The create path looks up `(idem_key, owner)` and returns the existing
row on a match, so a replay does not create a duplicate todo. Every other
todo mutation carries an `Idempotency-Key`; the one non-idempotent operation
(toggle) records it in `op_receipts` in the same transaction as the change.

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

## Offline-capable writes, not offline-first reads

Web clients store each mutation in a durable outbox before sending it and replay
it while the app is open; the desktop build becomes a NATS Leaf Node that keeps
its JetStream replica in sync while offline. Replays are deduplicated server-side
(creates by `idem_key`, the non-idempotent toggle by the `Idempotency-Key` receipt).

**What this is, precisely.** In the three-model split the 2026 guidance uses —
online-first with caching, offline-first, local-first — gogogo sits in the middle
for writes and the first column for reads: the *server stays the source of truth
for reads* (list, search, filters and indexes are queries; the page cache serves
the last visited view), while the *client owns writes offline* (the intent is
recorded locally and pushed later). It is **not** local-first: there is no client
read store, so the UI cannot list rows it has never synced, and a filter change
offline is refused rather than answered from a local replica. The desktop build
is the exception — with a Leaf Node the server is local, so reads work offline
too. `docs/architecture-decision.md` records why the middle column was chosen
over a local read store.

Set `OFFLINE_SYNC_ENABLED=false` for always-online deployments: it retires the
service worker (app-shell cache) and the NATS CRUD proxy. The write path is not
gated by the flag — the outbox runs on every page, and losing a queued write is
worse than the cache it protects.

The SCOPE unit `offline-sync` is a plugin (it can be trimmed); on this async
axis it is the mode above, not one of the seven layers.

## Related

- [Stack in layers, not silos](stack-layers.md)
- [Features](features.md)
- [Configuration](configuration.md)