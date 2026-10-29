# Unit contracts

> Generated from `internal/capabilities/registry.go` and the sources
> each unit owns — do not edit by hand.
> Regenerate: `go test ./internal/capabilities/ -run TestCapabilityContractsDocFresh -update`
> Classes and removal rules: [Scope taxonomy](scope-taxonomy.md).

One card per unit: what surface it serves, which routes it registers,
which generated queries it calls, how it is hidden at runtime, and what
removing it takes. Cards are the first thing to read after the SCOPE line
of the file you are about to touch.

## Registry at a glance

- **Units:** 39 — 13 core · 16 plugin · 10 feature
- **Offered to the installer:** 11
- **Source of truth:** `internal/capabilities/registry.go`; this doc is
  regenerated from it, so the counts above cannot drift.

## `appconfig` — core

env config (single source of truth for vars + defaults)

- **Owned:** `config`
- **Trim:** manual (no installer unit)
- **Note:** Core: every env var is documented here.

## `auth` — core

cookie sessions + middleware (flat auth + shared login UI)

- **Owned:** `internal/auth`, `features/authviews`
- **Routes:** `/api/auth/magic`, `/api/auth/magic/request`, `/api/auth/oauth/{provider}`, `/api/auth/oauth/{provider}/callback`, `/api/auth/password`, `/api/auth/password/reset`, `/api/auth/password/reset/request`, `/api/auth/sessions`, `/api/auth/sessions/revoke-all`, `/api/auth/sessions/{id}`, `/api/auth/signin/credential`, `/api/auth/signout`, `/api/auth/signup/credential`, `/api/auth/totp/begin`, `/api/auth/totp/disable`, `/api/auth/totp/login`, `/api/auth/totp/recovery`, `/api/auth/totp/verify`, `/api/auth/verify`, `/api/auth/verify/request`
- **Store calls:** `dbgen.ClearTOTP`, `dbgen.ConsumeRecoveryCode`, `dbgen.CreateMagicToken`, `dbgen.CreateRecoveryCode`, `dbgen.CreateSession`, `dbgen.CreateUser`, `dbgen.DeleteExpiredMagicTokens`, `dbgen.DeleteExpiredSessions`, `dbgen.DeleteMagicToken`, `dbgen.DeleteMagicTokensForUser`, `dbgen.DeleteMagicTokensForUserPurpose`, `dbgen.DeleteRecoveryCodesForUser`, `dbgen.DeleteSession`, `dbgen.DeleteSessionByID`, `dbgen.DeleteUserSessions`, `dbgen.GetMagicToken`, `dbgen.GetOAuthAccount`, `dbgen.GetSession`, `dbgen.GetUser`, `dbgen.GetUserByEmail`, `dbgen.InsertSecurityEvent`, `dbgen.LatestMagicTokenForUser`, `dbgen.LatestMagicTokenForUserPurpose`, `dbgen.LinkOAuthAccount`, `dbgen.ListSessionsByUser`, `dbgen.SetEmailVerified`, `dbgen.SetTOTPSecret`, `dbgen.UpdateSessionLastSeen`, `dbgen.UpdateUserPassword`, `dbgen.VerifyTOTPFlag`
- **Trim:** manual (no installer unit)
- **Note:** Core middleware; the login page is a removable UI (manual). internal/auth is the owned argon2id stack (gogogo_auth cookie); features/authviews holds the login page and shell controls. No framework middleware remains; the owned stack is the whole story.

## `crudwire` — core

todo CRUD wire protocol (subjects + payloads + publisher)

- **Owned:** `internal/natswire`
- **Trim:** manual (no installer unit)
- **Note:** Core, not plugin: chi handlers name these types in signatures even with NATS off (nil publisher), so trimming would not compile. The engines behind it (internal/nats) stay a plugin.

## `database` — core

flat data layer: Turso Database connection + migrations + sqlc queries

- **Owned:** `internal/db`, `internal/dbschema`
- **Trim:** manual (no installer unit)
- **Note:** Core: handlers call the generated dbgen.* functions directly; migrations under internal/dbschema/migrations are the schema (driver-free so zero-CGO targets share it; internal/db owns only the tursogo open).

## `demo-seed` — core

fresh-install demo account (one idempotent Signup)

- **Owned:** `internal/demo`
- **Store calls:** `dbgen.DeleteMagicTokensForUser`, `dbgen.DeleteUserSessions`, `dbgen.GetUserByEmail`, `dbgen.SetEmailVerified`, `dbgen.SetUserRole`, `dbgen.UpdateUserPassword`
- **Trim:** manual (no installer unit)
- **Note:** Core contract: a fresh clone has a way in. Boot (internal/server) and the native window both call it; Signup is idempotent.

## `hostgate` — core

host-capacity gate for heavy Go tests (skip on saturated boxes)

- **Owned:** `internal/hostgate`
- **Trim:** manual (no installer unit)
- **Note:** Core test support: heavy suites (trim-build repo copies + forked vets) skip with reason off-CI when the box is constrained, instead of wedging a shared host. CI never skips. Customize thresholds via HOSTGATE_* env, never remove.

## `instance` — core

stable instance identity for cross-instance durables and event origins

- **Owned:** `internal/instance`
- **Trim:** manual (no installer unit)
- **Note:** Core: the CRUD consumer's durable and every published event carry it (APP_INSTANCE_ID, else the hostname).

## `observability` — core

core observability (slog + tracing)

- **Owned:** `internal/observability`
- **Trim:** manual (no installer unit)
- **Note:** core infra, removal=core

## `queue` — core

SSE Hub (realtime fan-out) + goqite jobs + workers + retry

- **Owned:** `internal/queue`
- **Trim:** manual (no installer unit)
- **Note:** Core, and the Hub is the load-bearing half: every streaming handler (todo, genui, chat, notes, whiteboard, review, room) names *queue.SSEHub in its constructor, so no trimmed tree compiles without it — even with NATS off. The job queue shares the package because workers stream through the same hub.

## `rbac` — core

RBAC with Casbin (embedded model + policy, enforced at the mux)

- **Owned:** `internal/rbac`
- **Store calls:** `dbgen.CountCasbinRules`, `dbgen.DeleteAllCasbinRules`, `dbgen.DeleteCasbinRule`, `dbgen.DeleteCasbinRuleByID`, `dbgen.InsertCasbinRule`, `dbgen.ListCasbinRules`
- **Trim:** manual (no installer unit)
- **Note:** Wired middleware, not a dormant package: BootChi builds one enforcer and wraps the whole mux (pass-through when RBAC_ENABLED=false, enforced denies with 403 + warn log when true). Roles come from users.role (migration 0008, ADMIN_EMAILS promotes existing rows at boot). /config is admin-only in the handler too (rbac.RequireAdmin), so RBAC_ENABLED=false does not expose it; the console adds an ADMIN_EMAILS allowlist. The policy test pins every mux route for user-or-admin.

## `secrets` — core

age-decrypted env loader

- **Owned:** `internal/secrets`
- **Trim:** manual (no installer unit)
- **Note:** Core: config.Load reads through it. Customize, never remove.

## `server` — core

flat backend boot (Turso + auth + handlers + queues + workers)

- **Owned:** `internal/server`
- **Routes:** `/_ui/demo`, `/health`, `/sw.js`
- **Store calls:** `dbgen.CountUnreadInApp`, `dbgen.DeleteOpReceiptsBatchBefore`, `dbgen.DeleteSecurityEventsBatchBefore`, `dbgen.GetUserByEmail`, `dbgen.SetUserRole`
- **Trim:** manual (no installer unit)
- **Note:** Core: cmd/web and cmd/desktop both boot through BootChi/StartChi. attach JetStream, then serve.

## `shell` — core

one app shell (sidebar + module header + work area), the theme catalog, and the DaisyUI asset block every page renders

- **Owned:** `internal/ui`, `internal/uiregistry`, `internal/icon`, `internal/themes`, `internal/demo`
- **Store calls:** `dbgen.DeleteMagicTokensForUser`, `dbgen.DeleteUserSessions`, `dbgen.GetUserByEmail`, `dbgen.SetEmailVerified`, `dbgen.SetUserRole`, `dbgen.UpdateUserPassword`
- **Trim:** manual (no installer unit)
- **Note:** Core: every module page renders a body inside ui.Shell, and the shell renders the DaisyUI asset block on every page. Visual variants are DaisyUI themes (docs/ui-themes.md), not bundles.

## `app` — plugin

AppContext (dependency bundle for the native window)

- **Owned:** `features/app`
- **Trim:** manual (no installer unit)
- **Note:** Plugin, not core: cmd/web never links it — handlers take their deps directly, so only cmd/gui (the gogpu/ui proof) reads Context. Deleting the package drops that second frontend, never the web app.

## `capabilities` — plugin

capability registry + installer engine (CLI and MCP share it)

- **Owned:** `internal/capabilities`, `internal/installer`
- **Trim:** manual (no installer unit)
- **Note:** Meta-capability: deleting it breaks cmd/gogogo, not the web binary.

## `collab` — plugin

Loro CRDT transport: DocStore, sync workers, presence

- **Owned:** `internal/collab`
- **Store calls:** `dbgen.DeleteSnapshot`, `dbgen.GetSnapshot`, `dbgen.UpsertSnapshot`
- **Trim:** manual (no installer unit)
- **Why that class:** Bundled into the whiteboard unit: whiteboard embeds collab types (no-compile without it) and collab without whiteboard was dead until notes arrived as the second consumer (features/notes shares the transport on its own subject space). Split trigger met — see the notes entry: trimming the whiteboard bundle removes collab, which notes needs.
- **Note:** Shared by whiteboard + notes on the flat stack (their chi slices wire the transport). Snapshot durability is runtime: COLLAB_SNAPSHOTS=memory keeps snapshots process-local (draw without Turso writes; docs start empty after a restart). Files is empty on purpose: the importers outside internal/collab are EDITED (line drops), never deleted — internal/server/chi.go is core.

## `components` — plugin

shared UI helpers (Toast + OfflineBanner)

- **Owned:** `internal/components`, `features/todo/components`
- **Trim:** manual (no installer unit)
- **Why that class:** Every page layout calls into it; deletion is manual.

## `credits` — plugin

AI credits + BYOK ledger (ai-credits)

- **Owned:** `features/credits`
- **Routes:** `/api/ai/request`, `/api/byok/*`, `/api/credits`, `/api/credits/checkout`, `/api/credits/stripe-webhook`
- **Runtime off:** `CREDITS_ENABLED=false (already the default)`
- **Trim:** offered to the installer (a scaffold's keep lists/presets drop it)
- **Note:** If Stripe top-ups were the only stripe-go user, `go mod tidy` drops stripe-go too.
- **Warning:** Todo AI Suggest becomes unmetered (still works with GOAI_API_KEY, no ledger).

## `dagnats` — plugin

durable workflows (DagNats over JetStream) + onboarding worker

- **Owned:** `internal/dagnats`, `features/onboarding`, `internal/nats/single_nats_test.go`, `cmd/web/dagnats.go`, `cmd/web/dagnats_reminder_test.go`, `cmd/web/start_nats_test.go`
- **Routes:** `/api/onboarding/start`
- **Runtime off:** `DAGNATS_ENABLED=false`
- **UI signal:** `todo.Signals.DagNatsEnabled`
- **Trim:** offered to the installer (a scaffold's keep lists/presets drop it)
- **Note:** Deletion removes the engine + onboarding worker. The workflow tab, button, and hints auto-hide via the dagnatsUIEnabled signal (registration truth, not config truth).
- **Warning:** Runtime alternative (no deletion): DAGNATS_ENABLED=false.

## `datastar` — plugin

Datastar rendering helpers

- **Owned:** `internal/datastar`
- **Trim:** manual (no installer unit)
- **Why that class:** Every .templ page renders through it; deletion is manual.

## `delivery` — plugin

delivery transport seam: channels (email today) carried by a provider (smtp or an external notify engine)

- **Owned:** `internal/delivery`, `internal/server/delivery.go`
- **Runtime off:** `DELIVERY_TRANSPORT=off`
- **Trim:** manual (no installer unit)
- **Note:** Without it the inbox still works: the form drops the external channel checkbox, and a delivery that asked for one is recorded skipped/provider. The template ships in_app and email only; other channel values are data a provider may know, not a gogogo surface. in_app is never a sender, which keeps one owner for the inbox.

## `genkit` — plugin

typed AI flows (Genkit Go): orchestration + traces above internal/llm

- **Trim:** manual (no installer unit)
- **Why that class:** Single consumer (review): trimming the review unit plus `go mod tidy` drops github.com/firebase/genkit/go from the build. Generation reaches providers only through llm.Client, so no provider SDK rides along. Runtime-off needs no flag — without GOAI_API_KEY the flows 503 and the UI hides.

## `genui` — plugin

generative UI: the Chat demo renders LLM answers as Templ directives over the SSE hub

- **Owned:** `features/genui`, `internal/server/genui_stubs_test.go`, `features/todo/handlers/genui_panel.go`, `features/todo/handlers/genui_panel_test.go`, `features/todo/handlers/genui_owner_test.go`, `features/todo/handlers/panel_action.go`, `features/todo/handlers/panel_action_test.go`, `features/config/components/safe_view_catalog_test.go`, `features/todo/panel.go`, `features/todo/panel_filter_test.go`, `features/todo/components/panel.templ`, `features/todo/components/panel_templ.go`, `features/todo/components/panel_delete_test.go`, `web/resources/static/pele.global.js`, `web/resources/static/diagram.js`
- **Store calls:** `dbgen.ListTodosByOwner`
- **Trim:** offered to the installer (a scaffold's keep lists/presets drop it)
- **Note:** Deletion removes the Chat demo (one bundle: chat imports genui components, so chat without genui does not compile — same honest shape as whiteboard+collab+notes). The Chat sidebar entry is stripped automatically.
- **Warning:** ci-local smoke: export SMOKE_FEATURES without genui (e.g. SMOKE_FEATURES=todo,room,login) or the harness fails on the trimmed route.

## `goakt` — plugin

entity actors (GoAkt rooms: roster + presenter lock)

- **Owned:** `internal/goakt`, `features/room`, `cmd/web/goakt.go`, `cmd/web/goakt_test.go`
- **Routes:** `/room`, `/room/acquire`, `/room/crash`, `/room/heartbeat`, `/room/join`, `/room/leave`, `/room/release`, `/room/roster`, `/room/stream`
- **Runtime off:** `GOAKT_ENABLED=false`
- **Trim:** offered to the installer (a scaffold's keep lists/presets drop it)
- **Note:** Deletion removes the engine + room demo. The Room sidebar entry is stripped automatically; main.go boot lines go with the unit's mainStrips.
- **Warning:** Runtime alternative (no deletion): GOAKT_ENABLED=false.

## `llm` — plugin

GoAI LLM client behind an injectable interface

- **Owned:** `internal/llm`
- **Runtime off:** `Unset GOAI_API_KEY (suggest button hides)`
- **UI signal:** `todo.Signals.LLMEnabled`
- **Trim:** manual (no installer unit)
- **Why that class:** Load-bearing in core structs: features/app AppContext holds LLM *llm.Client (plus llm.Cfg/llm.Queue globals), TodoHandler calls llm methods directly, and credits implements llm.Biller (cascade). The goai dependency is shared with credits and internal/queue, so deletion saves source only, never deps. Runtime-off is the blessed path.

## `nats` — plugin

NATS JetStream: cross-instance broadcast + CRUD proxy + leaf nodes

- **Owned:** `internal/nats`
- **Runtime off:** `NATS_ENABLED=false (in-memory fallback)`
- **Trim:** manual (no installer unit)
- **Why that class:** 15+ importers carry NATS types in signatures; deletion is a manual refactor, not line strips.

## `offline-sync` — plugin

durable page outbox + app-shell cache (idempotent replay, NATS CRUD proxy)

- **Owned:** `web/resources/static/offline-outbox.js`, `web/resources/static/sw.js`
- **Runtime off:** `OFFLINE_SYNC_ENABLED=false`
- **Trim:** manual (no installer unit)
- **Why that class:** Spans a config struct field, the outbox, the worker, and templ registration; deletion is manual.

## `search` — plugin

search layers (substring, fuzzy, BM25) behind one seam

- **Owned:** `internal/search`, `features/todo/handlers/search_source.go`, `features/todo/handlers/search_source_test.go`, `features/todo/handlers/search_test.go`, `features/todo/handlers/search_mode_test.go`, `features/todo/handlers/search_heal.go`, `features/todo/handlers/search_heal_test.go`, `internal/search/sqlindex_inventory.go`, `internal/server/boot_search.go`
- **Store calls:** `dbgen.ListTodosByOwner`, `dbgen.ListTodosForIndex`, `dbgen.SearchTodosByOwner`
- **Trim:** offered to the installer (a scaffold's keep lists/presets drop it)
- **Note:** No external engine on purpose: this driver ships no FTS5 and no Turso native FTS (tursogo v0.8.2/v0.8.3/v0.9.0-pre.1), and a second index store would put a row's truth in two places.
- **Warning:** A consumer that loses this keeps the SQL substring layer: it holds a search.Searcher and degrades (that is why it is swappable).

## `sounds` — plugin

UI sound feedback (cuelume, vendored)

- **Owned:** `features/sounds`, `web/resources/static/cuelume`, `web/resources/static/cuelume.js`
- **Trim:** offered to the installer (a scaffold's keep lists/presets drop it)
- **Note:** Call sites are stripped automatically (this used to be manual).
- **Warning:** data-cuelume-* attributes stay in the markup but are inert without cuelume.js.
- **Warning:** The installer re-runs `go tool templ generate`; if that fails, run `make templ` manually before building.

## `account` — feature

authenticated /account security page

- **Owned:** `features/account`
- **Routes:** `/account`, `/account/password`, `/account/sessions/revoke`, `/account/sessions/revoke-all`, `/account/totp/begin`, `/account/totp/disable`, `/account/totp/recovery`, `/account/totp/verify`, `/account/verify`
- **Store calls:** `dbgen.ListSessionsByUser`
- **Depends on:** sounds
- **Trim:** manual (no installer unit)
- **Note:** Core identity UX; remove manually with the menu link in features/authviews/controls.templ.

## `chat` — feature

conversation demo (persisted history + catalog answers on genui)

- **Owned:** `features/chat`
- **Routes:** `/api/chat/ask`, `/api/chat/clear`, `/api/chat/stream`, `/chat`
- **Depends on:** genui
- **Trim:** manual (no installer unit)
- **Why that class:** Bundled into the genui unit: chat imports genui components (no-compile without it) — one unit is the honest shape, same as whiteboard+collab+notes.

## `config-view` — feature

admin-only /config view + user role/session actions

- **Owned:** `features/config`
- **Routes:** `/config`, `/config/audit.csv`, `/config/users/revoke`, `/config/users/role`
- **Store calls:** `dbgen.CountAdmins`, `dbgen.GetUserByEmail`, `dbgen.ListSecurityEventNames`, `dbgen.ListSecurityEventsByEvent`, `dbgen.ListSecurityEventsPage`, `dbgen.ListUsers`, `dbgen.SecurityEventCountsSince`, `dbgen.SetUserRole`
- **Depends on:** sounds
- **Trim:** offered to the installer (a scaffold's keep lists/presets drop it)
- **Warning:** The Config rail and account-menu entries are stripped automatically.

## `landing` — feature

public entry page on GET / (inside the shared shell)

- **Owned:** `features/landing`
- **Routes:** `/`
- **Depends on:** sounds
- **Trim:** offered to the installer (a scaffold's keep lists/presets drop it)
- **Warning:** GET / will 404 (landing owned the only root route); the shell brand is retargeted to /todo, which is kept.

## `notes` — feature

shared plain-text notes (server-owned Loro Text + SSE)

- **Owned:** `features/notes`
- **Routes:** `/api/notes/fragment`, `/api/notes/{docID}/caret`, `/api/notes/{docID}/delete`, `/api/notes/{docID}/leave`, `/api/notes/{docID}/op`, `/api/notes/{docID}/stream`, `/api/notes/{docID}/typing`, `/notes`, `/notes/new`, `/notes/{docID}`
- **Store calls:** `dbgen.DeleteNote`, `dbgen.GetNoteMeta`, `dbgen.ListNotes`, `dbgen.UpsertNote`
- **Depends on:** collab, sounds
- **Trim:** manual (no installer unit)
- **Note:** Manual removal (or trim the whiteboard unit, which covers whiteboard+collab+notes): delete the dir + the notes roots in buildChiMux (internal/server/chi.go), remove the Notes sidebar entry in internal/ui/modules.go, and remove this entry.

## `notifications` — feature

inbox + durable reminders (DagNats sleep → delivery row)

- **Owned:** `features/notifications`, `internal/server/inbox_badge.go`
- **Routes:** `/api/notifications/badge`, `/api/notifications/read-all`, `/api/notifications/remind`, `/api/notifications/stream`, `/api/notifications/{id}/read`, `/notifications`
- **Store calls:** `dbgen.CountNotificationsByChannelStatus`, `dbgen.CountUnreadInApp`, `dbgen.CreateNotification`, `dbgen.ListNotificationsForOwner`, `dbgen.MarkAllNotificationsRead`, `dbgen.MarkNotificationRead`, `dbgen.SetNotificationStatus`
- **Depends on:** sounds
- **Trim:** manual (no installer unit)
- **Why that class:** Removal is manual: delete this unit, the sidebar entry in internal/ui/modules.go (Pinned), the rail count middleware and the session-stream feed in internal/server/inbox_badge.go plus their r.Use / SetSessionStreamExtras lines in internal/server/chi.go and internal/server/delivery.go, the operator middleware's engine argument in chi.go (the console link reads this handler's Wired), the badge refresh button in internal/ui/inbox.templ and its route in the handler, the reminder definition in internal/dagnats/reminder.go, the reminder-seed and notification-deliver workers plus the onboarding welcome call and the startDagNats argument in cmd/web/dagnats.go. Migration 0015 stays (migrations are append-only; drop the table with a new one if the data must go). The transport seam is the separate `delivery` plugin.

## `review` — feature

review loop demo (writer → critic → classifier judge, streamed verdict)

- **Owned:** `features/review`
- **Routes:** `/api/review/run`, `/review`
- **Depends on:** genkit
- **Trim:** offered to the installer (a scaffold's keep lists/presets drop it)
- **Note:** No UISignal claim by design: the run UI gates on the todo.Signals ReviewEnabled field at runtime (keyless hides it), but the only .templ reader ships inside features/review — claiming it would fail the trimmed-tree conformance, the same reason SimulatedLLM is unclaimed. Trim removes the sidebar entry, so no dead UI either way. DependsOn lists genkit only: llm is never trimmed (runtime-off is blessed), so declaring it would break `gogogo add review` (add fails on deps no unit provides) for zero mechanical gain — the LLM requirement rides the Summary and docs instead.
- **Warning:** The Review sidebar entry is stripped automatically.
- **Warning:** ci-local smoke: export SMOKE_FEATURES without review (e.g. SMOKE_FEATURES=todo,room,login) or the harness fails on the trimmed route.

## `room` — feature

room presence demo (one grain per room, crash hook included)

- **Owned:** `features/room`
- **Routes:** `/room`, `/room/acquire`, `/room/crash`, `/room/heartbeat`, `/room/join`, `/room/leave`, `/room/release`, `/room/roster`, `/room/stream`
- **Trim:** offered to the installer (a scaffold's keep lists/presets drop it)
- **Warning:** The Room sidebar entry is stripped automatically.

## `todo` — feature

Todo MVC demo (reference implementation)

- **Owned:** `features/todo`
- **Routes:** `/api/todos`, `/api/todos/completed/delete`, `/api/todos/events`, `/api/todos/fragment`, `/api/todos/retry-demo`, `/api/todos/search`, `/api/todos/stream`, `/api/todos/suggest`, `/api/todos/suggest-simulated`, `/api/todos/{id}/confirm-delete`, `/api/todos/{id}/delete`, `/api/todos/{id}/toggle`, `/login`, `/logout`, `/todo`
- **Store calls:** `dbgen.ClaimOpReceipt`, `dbgen.CreateTodo`, `dbgen.DeleteCompletedByOwner`, `dbgen.DeleteTodo`, `dbgen.DeleteTodoByID`, `dbgen.GetTodo`, `dbgen.GetTodoByIdemKey`, `dbgen.GetTodoVisible`, `dbgen.GetUser`, `dbgen.GetUserByEmail`, `dbgen.ListCompletedIDsByOwner`, `dbgen.ListTodosByOwner`, `dbgen.ListTodosForIndex`, `dbgen.ListVisibleTodos`, `dbgen.SearchTodosByOwner`, `dbgen.SetTodoCompletedByID`
- **Trim:** manual (no installer unit)
- **Why that class:** Reference implementation — remove manually later per docs/scope-taxonomy.md.

## `whiteboard` — feature

collaborative canvas (Loro + Rough.js + presence)

- **Owned:** `features/whiteboard`
- **Routes:** `/api/whiteboard/{docID}/delete`, `/api/whiteboard/{docID}/presence`, `/api/whiteboard/{docID}/snapshot`, `/api/whiteboard/{docID}/stream`, `/api/whiteboard/{docID}/update`, `/api/whiteboards/fragment`, `/whiteboard`, `/whiteboard/new`, `/whiteboard/{docID}`
- **Store calls:** `dbgen.BumpBoardVersion`, `dbgen.DeleteBoard`, `dbgen.GetBoardMeta`, `dbgen.ListBoards`, `dbgen.UpsertBoard`
- **Depends on:** collab, sounds
- **Trim:** offered to the installer (a scaffold's keep lists/presets drop it)
- **Warning:** The Whiteboard sidebar entry is stripped automatically.
- **Warning:** ci-local smoke: export SMOKE_FEATURES without whiteboard (e.g. SMOKE_FEATURES=todo,room,login) or the harness fails on the trimmed route.

