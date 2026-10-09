# Features

Every capability is always compiled. The headline is the **feature**; the
second column names the **technology** that powers it.

| Feature | Powered by | Runtime opt-out | What it does |
|---|---|---|---|
| **Auth** | PocketBase | — (middleware stays; login UI removable) | Cookie sessions, demo login, per-request user loading |
| **Todo app + realtime** | PocketBase realtime + SSE Hub | — | DB actions (create/toggle/delete) stream per-user via the `owner` rule. SSE Hub carries ephemeral signals (toasts, clients count, AI suggest) |
| **Background jobs + retry** | `goqite` + `retry-go` | — | Background jobs with backoff/jitter. Stepper UI streamed via SSE (`techStep` / `techPhase`) |
| **AI Suggest** | GoAI | `GOAI_API_KEY` unset | LLM call from the todo UI; button hidden when no key. Stepper signals `aiStep` / `aiPhase` |
| **AI credits + BYOK** | [ai-credits](https://github.com/calionauta/ai-credits) | `CREDITS_ENABLED=false` | Optional plugin: meter Todo AI Suggest with reserve/settle, expose balances/top-ups, and proxy a user's encrypted provider key through an OpenAI-compatible BYOK relay |
| **Collaborative whiteboard** | Loro CRDT + Rough.js + NATS | — | Canvas, SSE + NATS broadcast, **live in-progress drawing** (the shape under the pointer is relayed on the volatile presence channel so peers watch it grow, then committed by the `add` op on release), offline-first outbox replay, PocketBase-persisted snapshots, server-stamped cursor identity (roster and cursors kept in separate maps; idle cursors expire on an 8 s TTL) |
| **Shared notes** | Loro Text (server-owned) + SSE Hub | — | Plain-text collab notes: character ops merge server-side, resolved text streams to peers, presence + typing + line carets (each caret measured against the reporter's own text snapshot, so a dot never lands on the wrong line while the two editors are briefly diverged), live index via PB realtime, snapshots in the `notes` collection |
| **Durable workflows** | DagNats over JetStream | `DAGNATS_ENABLED=false` | JSON workflows — HTTP API on `:8090`, durable state on `:4222` (e.g. `WelcomeOnboarding`) |
| **Room presence (entity actors)** | GoAkt grains (standalone) | `GOAKT_ENABLED=false` | One grain per room: heartbeat roster + exactly-one presenter lock, supervised with restart budget. Demo at `/room/` with a crash hook |
| **Multi-instance realtime** | NATS JetStream | `NATS_ENABLED=false` | JetStream fan-out for todo + whiteboard sync across >1 instance behind a LB |
| **Hybrid offline sync** | Service Worker + NATS Leaf Node + idempotency | `OFFLINE_SYNC_ENABLED=false` | NATS CRUD proxy + Service Worker offline queue (default on). Desktop edges publish CRUD ops via JetStream; web clients use Service Worker + Background Sync |
| **UI skins (pluggable)** | DaisyUI v5 + BasecoatUI | `UI_SKIN` | DaisyUI (default) or BasecoatUI (shadcn-style OKLCH tokens). Switch via env var or `?skin=`. See [UI skins](ui-skins.md) |
| **Pluggable persistence** | `pb` / `crdt` EntityStore | `ENTITY_STORE` | `pb` (default: PocketBase records + admin UI works) or `crdt` (Loro per-owner doc + JetStream transport). Same `EntityStore[T]` interface |
| **Desktop-edge sync** | NATS Leaf Node | `NATS_LEAFNODE_URL` unset | Leaf-Node JetStream replication of Loro updates for desktop/edge clients |
| **Landing page** | Templ | — | The app's own public page on `GET /` (not the GitHub Pages promo site, which is never installed). No auth, no DB. Todo demo lives at `/todo` |
| **Read-only config view** | Templ | — | Auth-gated `GET /config`: env-decrypted values, masked secrets, runtime constants. Never mutates state |

## Adding a new feature

1. Create `features/<name>/` with its HTTP handlers + Templ components.
2. Wire it in `router/router.go` → `Init()` with a single function call.
3. Use **goqite** for async work, the **SSE Hub** for user-facing feedback, and
   **Datastar** for the reactive frontend.
4. Add a `SCOPE:` annotation — `make check-scope` enforces it.
5. Add a `RegisterRoutes(se, deps)` function and call it from `router.Init`.

The [Todo feature](todo-example.md) is the full reference implementation.

## The surfaces you get

| Surface | URL | Notes |
|---|---|---|
| Landing page | `/` | Public. Same page for guests and signed-in users |
| Todo demo | `/todo` | Auth-gated. Seeded demo account |
| Whiteboard | `/whiteboard` | Auth-gated |
| Room demo | `/room` | Auth-gated. Heartbeat roster + presenter lock owned by a GoAkt grain |
| Config view | `/config` | Auth-gated, read-only |
| PocketBase admin | `/_/` | Superuser auth |
| DagNats console | `:8090` / `/dagnats/` | Workflow runs and steps |

Details in [Admin & Dashboard](admin-dashboard.md).

## Removing a feature

Every feature package carries a `SCOPE:removal=feature` annotation. The rule:
delete the package directory, delete its dependents, remove the wiring call
from `router/router.go` → `Init()`, and — if it was a plugin — remove the
`start*` call in `cmd/web/main.go`. Full checklist in
[Scope taxonomy](scope-taxonomy.md#removing-a-component).

## Client weight

Vendored JS per feature, measured deterministically (`wc -c` raw,
`gzip -nc` for wire; rounded up). CSS is one shared bundle and is not
attributed per feature.

| Feature | Files | Raw | Gzip |
|---|---|---|---|
| Todo, Room, Landing, Config | inline only (no extra JS) | 0 KB | 0 KB |
| Shared notes | `notes.js` | 28 KB | 10 KB |
| Whiteboard | `whiteboard.js` + `rough.min.js` | 54 KB | 18 KB |
| Shared core (every page) | `datastar.js` + `theme.js` | 40 KB | 16 KB |

Whiteboard and notes pages additionally load the shared
`throttle.js` (~2 KB raw, ~1 KB gzip) — leading + trailing throttle
for cursor/caret reports, one implementation for both features.

Split rule (Datastar vs vanilla, enforced by review): list fragments
and UI state that patch cleanly go through the Datastar runtime (todo,
whiteboard list, notes index — every one of those pages loads
`datastar.js`, pinned by test); canvas rendering, textarea math, and
op batching stay vanilla JS (no declarative equivalent, and the offline
outbox needs exact control). Shared chrome counts as UI state: the
navbar logout form carries `data-on` handlers, so every page rendering
the navbar loads the runtime too (board, notes doc, room, config,
landing) — a `data-on`/`data-bind` attribute on a page without the
runtime is dead markup, and here it would silently skip the logout
service-worker cleanup.

Session visibility is shared the same way: the notes and whiteboard streams
open with a `collab.SessionEvent` frame, and pages without a raw stream
(todo, room) poll `/api/session`, so every tab raises the same
`components.SessionBanner` the moment a cookie expires — peer names never
silently degrade to client hashes with nothing on screen explaining why, and
the banner never auto-redirects (unsent work would be stranded). It is
opt-in per page via the render-time `authed` flag, so a public page never
nags about a session it never had.

Method: `for f in <files>; do wc -c < $f; gzip -nc $f | wc -c; done`.
`datastar.js` is 34 KB raw / ~13 KB gzip — that is the "~12 KiB client"
in the README (wire size). Icons (`iconify`, 24 KB raw) load only on
pages that use them; sounds (`cuelume.js`, 10 KB) only with the sounds
plugin.

## Related

- [Seven async layers](async-layers.md) — the topology behind these capabilities.
- [The Todo example](todo-example.md) — the pattern to imitate.
- [Desktop & Mobile](desktop-mobile.md) — two more frontends over the same backend.