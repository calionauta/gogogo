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
| **Collaborative whiteboard** | Loro CRDT + Rough.js + NATS | — | Canvas, SSE + NATS broadcast, offline-first outbox replay, PocketBase-persisted snapshots |
| **Shared notes** | Loro Text (server-owned) + SSE Hub | — | Plain-text collab notes: character ops merge server-side, resolved text streams to peers, snapshots in the `notes` collection |
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

## Related

- [Seven async layers](async-layers.md) — the topology behind these capabilities.
- [The Todo example](todo-example.md) — the pattern to imitate.
- [Desktop & Mobile](desktop-mobile.md) — two more frontends over the same backend.