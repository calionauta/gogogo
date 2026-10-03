# Features

Every capability is always compiled. What you get out of the box:

| Capability | Runtime opt-out | What it does |
|-----------|----------------|--------------|
| **Todo app + PocketBase realtime** | — | DB actions (create/toggle/delete) stream through PocketBase realtime, per-user scoped via the `owner` rule. SSE Hub for ephemeral signals (toasts, clients count, AI suggest) |
| **Queue + retry** | — | `goqite` background jobs + `retry-go` (the "Queue + Retry" demo). Stepper UI streamed via SSE; uses signal-set `techStep` / `techPhase` |
| **AI Suggest** | `GOAI_API_KEY` unset | GoAI call from the todo UI; button hidden when no key. Stepper UI streamed via SSE; uses signal-set `aiStep` / `aiPhase` (kept independent from Queue + Retry's stepper signals) |
| **AI credits + BYOK** | `CREDITS_ENABLED=false` | Optional [ai-credits](https://github.com/calionauta/ai-credits) plugin: meter Todo AI Suggest with reserve/settle, expose balances/top-ups, and proxy a user's encrypted provider key through an OpenAI-compatible BYOK relay |
| **Collaborative whiteboard** | — | Loro CRDT + Rough.js canvas, SSE + NATS broadcast, offline-first outbox replay, PocketBase-persisted snapshots |
| **UI skins (pluggable)** | `UI_SKIN` | DaisyUI v5 (default), BasecoatUI (shadcn-style OKLCH tokens), or Morpheus (vendorized web components). Switch at runtime via `UI_SKIN` env var or `?skin=` query. See [UI skins](ui-skins.md) |
| **Landing page** | — | Public marketing page on `GET /` (project tagline + a single CTA). Does **not** require auth and does **not** read the database. The todo demo lives at `/todo` |
| **Read-only config view** | — | Auth-gated `GET /config` shows what the binary has decided to do: env-decrypted values, masked secrets, runtime constants. Never mutates state |
| **Pluggable persistence** | `ENTITY_STORE` | `pb` (default: PocketBase records + admin UI works) or `crdt` (Loro per-owner doc + JetStream cross-instance transport). Same `EntityStore[T]` interface, swapped via one env var |
| **Multi-instance realtime** | `NATS_ENABLED=false` | NATS JetStream fan-out for todo + whiteboard sync across >1 instance behind a LB |
| **Durable workflows** | `DAGNATS_ENABLED=false` | DagNats JSON workflows — HTTP API on `:8090`, durable state on JetStream `:4222` (e.g. `WelcomeOnboarding`) |
| **Desktop-edge sync** | `NATS_LEAFNODE_URL` unset | Leaf-Node JetStream replication of Loro updates for desktop/edge clients |
| **Hybrid offline sync** | `OFFLINE_SYNC_ENABLED=false` | Disables the NATS CRUD proxy + Service Worker offline queue (default on). Desktop edges publish CRUD ops via NATS JetStream and the server's CrudConsumer writes to PocketBase; web clients use Service Worker + Background Sync. Set to `false` for always-online deployments and zero code paths are traversed |

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

- [Six async layers](async-layers.md) — the topology behind these capabilities.
- [The Todo example](todo-example.md) — the pattern to imitate.
- [Desktop & Mobile](desktop-mobile.md) — two more frontends over the same backend.