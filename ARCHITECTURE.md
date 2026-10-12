# Architecture — LLM Entrypoint

> This file is the **canonical entrypoint for LLM agents** navigating the template.
> Each row describes one component: where it lives, what it depends on, and how to remove or replace it.

---

## Principles

- **Unified build.** One `go build ./cmd/web` compiles everything — no build tags. The database drivers (Turso `turso`, ncruces `sqlite3`) are always compiled in, so nothing is gated behind a tag. Opt out at runtime with env vars (`NATS_ENABLED=false`, `DAGNATS_ENABLED=false`).
- **Features in `features/`**, infrastructure in `internal/`. Features depend on infra, never the reverse.
- **Wiring in `internal/server/chi.go` → `BootChi` + `buildChiMux`**. Every feature is mounted by one composition function.
- **Startup order in `cmd/web/main.go`** (see diagram below).

---

> **SCOPE annotations in code.** Every non-test, non-generated `.go` file under `internal/` and `features/` carries a leading doc-comment line declaring its SCOPE on **two orthogonal axes**: `// SCOPE:layer=<infra|feature>,removal=<core|plugin|feature> — <description>`. The `layer` axis says where the code lives (cross-cutting infra vs product feature). The `removal` axis says what happens if you delete it (binary breaks, binary loses capability, or pure demo). This two-axis scheme eliminates the prior ambiguity where `SCOPE:core - REMOVE if not using NATS` and `SCOPE:core - DO NOT REMOVE - SSE Hub` shared the same label.

> The `cmd/check-scope` Go program walks every `.go` file in `internal/` and `features/` and asserts the canonical SCOPE line is present in the leading doc-comment group. `make ci-local` runs it via the `check-scope` target. The pre-commit hook runs it conditionally when staged files match `^(internal|features)/.*\.go$`. Migrating an existing file uses `python3 scripts/migrate-scope.py` (idempotent). The `SCOPE` annotations in source files are the authoritative reference; this table is a summary.

> **Native boundary (not a layer).** `native` is a reserved implementation boundary, not a fourth SCOPE value. There is no Zig code or toolchain in this repo. If a justified case ever arrives, the Go API lives in its normal package (`internal/<thing>/` with the usual SCOPE) and Zig sources stay inside it behind a small C ABI — see `docs/exception-to-go.md`. Do not create a top-level `native/` directory speculatively.

## Layer taxonomy (SCOPE)

Every component belongs to one of three layers, matching the `SCOPE:` annotation in each source file:

| SCOPE | Meaning | You would… |
|:------|:--------|:-----------|
| **Core** 🔴 `SCOPE:layer=…,removal=core` | Binary does not work without it. | Customize, never remove. |
| **Plugin** 🟡 `SCOPE:layer=…,removal=plugin` | Binary works but loses capability if removed. Clear removal instructions inline. | Swap for an equivalent, or delete + remove wiring call. |
| **Feature** 🟢 `SCOPE:layer=…,removal=feature` | A demo/add-on. Depends on other packages (listed in comment). | Delete the package + dependent packages + remove wiring call. |

---

## Features (features/)

These are **product-level demos** — what the end user sees. All are Feature layer: you can remove, replace, or keep them.

| Feature | Package | SCOPE | Router wiring | Remove by |
|---------|---------|:-----:|---------------|-----------|
| **Auth** | `internal/auth/` + `features/authviews/` | 🟢/🔴 | `a.Handler()` mounted at `/api/auth/` in `buildChiMux` | Delete the login UI package; the middleware stays |
| **Landing page** | `features/landing/` | 🟢 FEATURE | Mounted at `/` in `buildChiMux` | Delete package + remove call. The todo demo is at `/todo`; root becomes 404 or your replacement. |
| **Read-only config view** | `features/config/` | 🟢 FEATURE | Mounted at `/config` in `buildChiMux` | Delete package + remove call. Page is auth-gated; secrets are masked via `mask.go` before render. |
| **Todo** | `features/todo/` | 🟢 FEATURE | `todoH` mounted at `/todo` + `/api/todos` in `buildChiMux` | Delete package + remove block |
| **Whiteboard** | `features/whiteboard/` + `internal/collab/` | 🟢 FEATURE | `wbH` mounted at `/whiteboard` + `/api/whiteboard/` in `buildChiMux` (one argument per line, so a trim drops exactly its lines) | Delete both dirs + remove call |
| **Notes** | `features/notes/` (shares `internal/collab/`) | 🟢 FEATURE | `notesH` mounted at `/notes` + `/api/notes` in `buildChiMux` | Delete dir + remove call (+ navbar link, seed, capability entry) |
| **Onboarding** | `features/onboarding/` + `internal/dagnats/` | 🟢 FEATURE | `mountOnboarding` after the composition (no-op when `DAGNATS_ENABLED=false`) | Delete both + remove call |
| **Room demo** | `features/room/` + `internal/goakt/` | 🟢 FEATURE | `roomH` mounted at `/room` in `buildChiMux` (nil-able; no-op when `GOAKT_ENABLED=false`) | Delete both dirs + remove call |
| **Persistence (single store)** | `internal/db/` (migrations + sqlc queries) | 🔴 CORE | `db.Open(cfg)` in `BootChi` | Core data layer; customize, never remove |

> **⚠️ Auth is a mixed package.** The **login UI** (login page, navbar) is 🟢 FEATURE — replace with OAuth, SSO, etc. The **auth middleware** (`LoadAuthFromCookie`) is 🔴 CORE — the app's security model depends on it. They live in the same package for cohesion; if you replace the UI, keep the middleware functions.

---

## Infrastructure (internal/)

These are the **plumbing layers**. Each is independently replaceable.

| Component | Package | SCOPE | Startup | Swap / Remove by |
|-----------|---------|:-----:|---------|------------------|
| **Database + Auth** (Turso/SQLite + owned sessions) | `internal/db/` + `internal/auth/` | 🔴 CORE | `db.Open(cfg)` + `auth.New(...)` in `BootChi` | Replace with your own DB + auth stack |
| **Config** | `config/` | 🔴 CORE | `config.Load()` in `main.go` | Add/remove env vars |
| **Queue + SSE Hub** (goqite) | `internal/queue/` | 🔴 CORE | Built in `BootChi` (chi queue + workers) | Replace goqite with Redis; SSE Hub is in `ssehub.go` |
| **Event bus: NATS JetStream** | `internal/nats/` | 🟡 PLUGIN | `startNATS(cfg)` in `main.go` | Remove `startNATS` call; falls back to in-memory fan-out via SSE Hub |
| **CRUD proxy (offline sync)** | `internal/nats/` + `internal/natswire/` | 🟡 PLUGIN | Installed by `AttachJetStream` (publisher + consumer) | Detach call; toggle via `OFFLINE_SYNC_ENABLED=false` (default on) |
| **DagNats** (workflows) | `internal/dagnats/` | 🟡 PLUGIN | `startDagNats(cfg, ...)` in `main.go` | Remove call + delete package |
| **CRDT + Sync** (Loro) | `internal/collab/` | 🟡 PLUGIN | Wired in `BootChi` (persister + DocStore shared by whiteboard/notes) | Delete with the whiteboard unit (whiteboard+collab+notes) |
| **SSE helpers** (Datastar) | `internal/datastar/` | 🟡 PLUGIN | Imported by handlers | Replace with your own SSE rendering |
| **Secrets** (age) | `internal/secrets/` | 🔴 CORE | `secrets.Load(appName)` in `config.Load()` | Remove call; env vars work without it |
| **LLM client** (GoAI) | `internal/llm/` | 🟡 PLUGIN | Built in `BootChi` | Remove env var; UI auto-hides the Suggest button. *The package stays if you add your own AI feature — only the demo Suggest route is removable.* |
| **UI skins** (pluggable) | `web/skins/` (`daisyui`, `basecoat`) | 🟡 PLUGIN | Imported at init via `features/todo/components/skin_imports.go` blank imports; active skin read by `config.Skin` and `?skin=` query | Delete `web/skins/` directory + drop the blank imports in `skin_imports.go` + drop the `SkinSelector` call from the navbar. The handler's lazy fallback returns DaisyUI assets when no skin is registered. See [UI skins](#ui-skins). |

> **🔴 CORE** = keep or replace the whole stack.  
> **🟡 PLUGIN** = you could remove it and still serve pages, but lose cross-instance broadcast, async jobs, etc.  
> **🟢 FEATURE** = pure demo. Delete the package, remove the wiring call, nothing breaks.

## Offline strategy for record features (todo, CRUD rows)

Turso/SQLite is a server-side database — it **cannot work offline natively.** Features like the todo app that hit it directly fail when the client loses connectivity. Here is the recommended strategy for our stack:

### Recommended: Service Worker + Background Sync (Web)

```
Browser                          Server
┌──────────────────┐            ┌──────────────────────┐
│  IndexedDB cache │            │  Turso/SQLite         │
│  (Dexie.js)      │  ◄── HTTP ─┤  (source of truth)    │
│                  │            │                       │
│  Service Worker  │  ── POST ─►│  Record SSE           │
│  (Background     │  (replay   │  (when online)        │
│   Sync API)      │   offline) │                       │
└──────────────────┘            └──────────────────────┘
```

**How it works:**
1. A **Service Worker** intercepts all fetch requests to the app API
2. Online: requests pass through normally; responses are cached in an **IndexedDB** store
3. Offline: GET requests read from the cache; POST/PUT/DELETE requests are queued in IndexedDB
4. On reconnect, the Service Worker uses the **Background Sync API** to replay queued mutations
5. The record SSE stream auto-reconnects; the SW re-subscribes on the `online` event

**Pros:** ✅ Preserves record sync when online | ✅ Zero backend changes | ✅ SW survives tab close
**Cons:** ❌ Requires client-side JS (not available in all Wails webviews) | ❌ Conflict resolution is LWW only

### Alternative: Loro CRDT as sync layer (like the whiteboard)

For features that need **conflict-free offline editing** (not just offline queuing), use the same Loro CRDT architecture the whiteboard uses:

1. Store the feature's data as a Loro document (JSON-like map/list)
2. Persist Loro snapshots to Turso (the collab Persister)
3. Use SSE Hub (in-process) + NATS (cross-instance) for realtime sync
4. Loro CRDT handles merge conflicts automatically

**Pros:** ✅ True offline-first (edit anywhere, merge automatically) | ✅ CRDT convergence
**Cons:** ❌ Loses per-record push for record-level events | ❌ Data not directly queryable row-by-row via HTTP

### Decision: When to use each

| Scenario | Strategy | Why |
|----------|----------|-----|
| Simple CRUD (todo, forms) | SW + Background Sync | Record SSE works; LWW is fine for single-owner data |
| Collaborative editing (whiteboard, docs) | Loro CRDT | Conflict-free merge is essential for multi-user |
| Read-heavy, write-rare (catalog, settings) | SW cache-only | No offline writes needed; stale-while-revalidate is fine |

The **whiteboard already uses Loro CRDT**. For the **todo feature**, SW + Background Sync is the implemented path — `web/resources/static/sw.js` intercepts `/api/*` mutations, queues them in IndexedDB, and replays them via Background Sync on reconnect; the shared **`OfflineBanner`** (`internal/components/`) surfaces the offline/syncing/online state to the user. This preserves record sync while keeping the KISS offline-queue option.

**Replay dedup (unique `(owner, idem_key)` index).** The server generates row IDs, so a naive replay of a queued POST creates a duplicate. The fix: `createForm` sends a fresh `idem_key` UUID in the form body, and the create path looks up an existing row with the same `(idem_key, owner)` and returns it in place of the inbound create. The unique index `(owner, idem_key)` is the race-condition safety net: two concurrent requests racing the lookup see the second one fail at the DB layer. UPDATE/DELETE handlers are not covered: toggles are naturally idempotent (two flips cancel out), and delete-on-already-deleted is a benign 404. Onboarding start (DagNats workflow trigger) accepts a small duplicate-cost on replay — the second run creates a second set of example todos, but the durable workflow tracks them as separate runs.

---

### The three async layers — how they compose

```
                ┌──────────────────────────────────┐
                │  goqite queue (SQLite-backed)     │  ← Core: background jobs
                └─────────┬────────────┬───────────┘
                          │            │
                          ▼            ▼
              ┌──────────────────┐  ┌───────────────────┐
              │    SSE Hub       │  │  DagNats workflows│
              │ (in-process fan) │  │  (Plugin 🟡, :8090)  │
              └────────┬─────────┘  └────────┬──────────┘
                       │                     │
                       ▼                     ▼
              ┌──────────────────────────────────┐
              │         Browser SSE               │
              │        (Datastar)                 │
              └──────────────────────────────────┘

              ┌──────────────────────────────────┐
              │   NATS JetStream                 │  ← Plugin 🟡: cross-instance event bus
              │                                   │
              │  ▲ WebSyncWorker publishes ops    │
              │  ▲ DagNats persists workflow      │
              │  ▼ SyncWorker receives ops         │
              └──────────────────────────────────┘
```

- **goqite** (Core 🔴): every feature's async work. Worker pool streams progress via SSE Hub.
- **SSE Hub** (Core 🔴, in `internal/queue/ssehub.go`): in-process fan-out to browser tabs via Go channels. Per-client channels, replay buffer, backpressure. **Does not cross the NATS boundary** — it is purely in-process.
- **NATS JetStream** (Plugin 🟡): cross-instance event bus. Used by:
  - Whiteboard sync — shapes published directly via `WebSyncWorker.nc.Publish()` to subject `app.sync.<docID>` (bypasses SSE Hub entirely)
  - DagNats — workflow engine uses JetStream for durable state
  - Desktop Leaf Node — optional edge sync
  - *(Todos currently use the in-memory SSE Hub path, not the JetStream TODOS stream — the `JetStreamBroadcaster` exists in `internal/nats` but nothing wires it; cross-instance record convergence runs through the APP_CRUD stream + `AttachJetStream` instead.)*
- **DagNats** (Plugin 🟡): durable multi-step workflows as declarative JSON. Uses NATS JetStream for state.
- **GoAkt** (Plugin 🟡): entity actors (one grain per room: roster + presenter lock, supervised). Standalone, no network.

---

## UI skins (pluggable)

The same UI is rendered through one of three pluggable skins, compiled into the same binary. The active skin is selected by `cfg.Skin` (env `UI_SKIN`, default `"daisyui"`) or per-request via the `?skin=` query string. The navbar exposes a `SkinSelector` widget that updates the query param and reloads.

| Skin | Package | What it is | Source CSS |
|------|---------|-----------|------------|
| **DaisyUI** (default) | `web/skins/daisyui/` | DaisyUI v5 components over TailwindCSS; morph-friendly with Datastar | `src/css/input.css` → `app.min.css` |
| **Basecoat** | `web/skins/basecoat/` | BasecoatUI (shadcn-style) with shadcn-inspired OKLCH `@theme inline` tokens; native Basecoat JS runtime (`basecoat.initAll`) debounced via `requestAnimationFrame` for Datastar DOM morphing | `src/css/basecoat-input.css` → `basecoat.min.css` + `basecoat.min.js` |

**Plugin contract** (`web/skins/skin.go`). Every skin is a `Skin{Name, Assets}` value registered at init time via blank imports in `features/todo/components/skin_imports.go`. The dispatcher falls back to DaisyUI when the env value is unknown, logging a warning. Adding a fourth skin is: create `web/skins/<name>/`, register it from the import file, add a `make css-<name>` Makefile target.

**Skin-aware SSE patches.** Some fragments must render with the matching skin's HTML (e.g. Basecoat todo cards). The skin dispatcher threads the active skin name into the SSE patch so `MergeSignals` + `RenderAndPatch` produce skin-correct markup. See `web/skins/basecoat/todo_basecoat.templ`.

**Removal.** Delete `web/skins/`, drop the blank imports in `features/todo/components/skin_imports.go`, drop the `SkinSelector` call from the navbar. The handler's lazy fallback returns DaisyUI assets when no skin is registered.

---

## Startup order (cmd/web/main.go)

```
main.go
  ├─ config.Load()              ← 🔴 Core: reads env vars + age secrets
  ├─ server composition (BootChi) ← 🔴 Core: Turso + queue + mux
  │   └─ buildChiMux(...)         # one subtree per capability; landing `/` is the catch-all
  │       ├─ static files         🔴 Core
  │       ├─ auth routes          🟢 FEATURE UI + 🔴 CORE middleware
  │       ├─ todo routes          🟢 FEATURE
  │       ├─ onboarding mount     🟢 FEATURE (after the composition)
  │       ├─ whiteboard routes    🟢 FEATURE (shares DocStore + persister)
  │       └─ notes/room/chat/genui/credits routes (room nil-able)
  ├─ startDagNats(cfg, ...) ← 🟡 PLUGIN: boots engine (NATS on :4222)
  ├─ startGoAkt(cfg)            ← 🟡 PLUGIN: boots room actors (standalone, no network)
  ├─ startNATS(cfg)             ← 🟡 PLUGIN: connects or starts embedded NATS
  └─ Serve(addr)                ← 🔴 Core: serves HTTP
```

> **OfflineSync toggle.** The entire offline sync stack is gated by `cfg.OfflineSync.Enabled`. When `false`: the CRUD publisher stays nil, the CRUD consumer no-ops, and no NATS CRUD stream is created. Set `OFFLINE_SYNC_ENABLED=false` in production for an always-online deployment with zero overhead.

> **Note on startup order:** the flat stack boots the database and queue before the optional engines, so JetStream-dependent wiring attaches after `startNATS`, not during the initial composition. The whiteboard's `WebSyncWorker` publishes to NATS directly, which is why it works regardless of boot order.

---

## Adding a new feature

1. Create `features/<name>/` with your HTTP handlers + Templ components.
2. Add a `Routes()` method on your handler and mount its subtree in `buildChiMux`.
3. Add a "Remove by" comment.
4. Add a row to the Features table above.

New infra should go in `internal/<name>/`. Same pattern: create the package, wire it in `main.go` or `chi.go`, add a "Swap / Remove by" comment.

---

## File tree

```
cmd/web/main.go            Entry point (chi flat stack + goqite + SSE Hub + DagNats + NATS)
cmd/gui/main.go            Native window (gogpu/ui over the same Turso/sqlc store, no HTTP) 🟢 FEATURE
config/config.go           Env-based config (incl. UI_SKIN, BUILD_LABEL/COMMIT)
internal/db/               Turso open + migrate + sqlc queries
features/                  Demo features (🟢 FEATURE, except auth middleware 🔴)
  auth/                      Login/logout/cookie (UI 🟢, middleware 🔴)
  landing/                   Public marketing hero on GET / (🟢 FEATURE)
  config/                    Auth-gated read-only /config view (🟢 FEATURE, with masked secrets)
  todo/                      Todo MVC (the reference implementation)
    handlers/                  HTTP routes + SSE stream + onboarding
    components/                Templ components (layout, todo_item, todo_list, skin_imports) — offline indicator is the shared `OfflineBanner` in `internal/components/`
  whiteboard/                Collaborative canvas
internal/                  Infrastructure
  queue/                     goqite + SSE Hub + workers + retry + handler registry
  nats/                      Embedded NATS + JetStream broadcaster + CRUD proxy
  dagnats/                   DagNats client + workflow definitions
  collab/                    Loro CRDT + DocStore + sync workers + presence
  llm/                       GoAI LLM client + simulated client (SSE-streaming suggest)
  components/                Shared UI helpers (Toast + OfflineBanner offline indicator)
  datastar/                  Datastar SSE rendering helpers
  secrets/                   age-decrypted secrets loader
internal/server/chi.go       BootChi + buildChiMux (composition root)
web/
  resources/static/          Embedded JS/CSS assets (app.min.css, basecoat.min.css, sw.js, theme.js)
  skins/                     Pluggable UI skin registry (daisyui + basecoat) 🟡 PLUGIN
```

### Routing notes

- **`GET /api` and `GET /api/` are served in both forms** (registered with and without the trailing slash on the stdlib mux). A trailing slash would otherwise 404 against an exact subtree pattern; serving both keeps every client working.
- **Static assets mount per-subtree** (`/static/`, `/static/wb/`, `/static/notes/` — no wildcards). Longest match wins, so keep feature assets on their own subtree; using exact routes prevents the next maintainer's pattern from accidentally serving `web/resources/static/app.min.css.map` from the wrong origin.
- **Page-style handlers mount their subtree in `buildChiMux`** (todo, whiteboard, landing, config, SkinSelector); the landing `/` is the catch-all. Register at the composition site — there is no nested-hook mechanism, so a route registered anywhere else never fires.
