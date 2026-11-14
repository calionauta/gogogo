# Scope taxonomy

> **Removal vs distribution.** This page answers *what disappears if I
> delete this* — the removal class (`Kind`) on the registry row. It is **not**
> the distribution model. The recipe model (copy + declaration + receipt,
> [Extensibility](research/extensibility-2026-11.md), authoring contract
> [`skills/gogogo-recipe-authoring`](../skills/gogogo-recipe-authoring/SKILL.md))
> answers *how code is installed and owned*. The three classes below are
> the removal taxonomy; the site presents them as **Core / Servants /
> Recipes**.

Every removable-or-toggleable piece of the template is one row in
`internal/capabilities`, and the removal class is a field on that row. There is
no per-file header and no second table: a package's class comes from the row
that owns it, and a package no row owns fails the registry's coverage test. That
is the rule that makes the template safely trimmable — you never have to guess
whether a directory is load-bearing.

## The three removal classes

The row carries a single axis — the removal class (`Kind`) — with three values:

| Class (site name) | Meaning | You would… |
|------------|---------|-----------|
| `core` 🔴 (Core) | The web binary **or its gates** do not work without it. Some have a runtime opt-out via env vars. Includes the recipe engine (`internal/recipe`). | Customize, never remove. |
| `plugin` 🟡 (Servant) | Binary works but loses a capability. A servant serves other capabilities (jobs, metering, sounds) — no page disappears, but something stops working. | Swap for another implementation, or delete the package + wiring call (e.g. `BootChi` in `internal/server/chi.go`, `cmd/web/main.go`). |
| `feature` 🟢 (Recipe) | A demo/add-on and a **recipe**: a terminal user surface — a page or journey disappears (todo, whiteboard, landing, config). In-tree recipes mount through the same seam installed recipes use (`recipes.gen.go` / `units.gen.go`). | Keep as a reference while building your own, then remove, or uninstall the receipt. |

Three clarifications the table alone does not carry:

- **Core includes gate support.** `internal/hostgate` never links into the web
  binary, but every heavy suite and `ci-local` depends on it, so deleting it
  breaks the gates that ship the binary. That is the "or its gates" clause.
- **Plugin does not promise a one-line delete.** `internal/nats`, `internal/llm`
  and `internal/collab` are named in signatures, so removing one is a manual
  refactor that touches its consumers; the class answers *what disappears*, not
  *how mechanical the trim is*. A `core` unit is the one where removal is not
  coherent at all.
- **The servant wins ties.** A unit that both powers other surfaces and ships a
  page is a plugin when the capability is the servant and the page is its demo.
  `features/genui` is the precedent: the Chat page disappears with it,
  but the unit exists to render answers for chat, todo panels and review, so it
  is classified plugin (and bundled with `features/chat` for trimming).

## Recommendation (installer advisory)

`Capability.Recommended` marks recipes the registry advises at scaffold
time (today: `todo` only — the reference slice). `CatalogEntry.Recommended`
does the same for out-of-tree catalog recipes: the catalog maintainer flips
it without code changes. The installer prints `recommend:` in `--dry-run`;
nothing installs without explicit acceptance (`--install-recommended` or
`--install <ids>`). Bundled recommended recipes ship in the default set
(they *are* the template), so acceptance is recorded in the plan; catalog
recipes take the full copy + receipt + `recipes.gen.go` path on acceptance.

## Installer scope (scaffold prompt retired)

Removal class answers *what disappears*. The former scaffold prompt
(`default`/`opt-in`/`manual`, see [Scaffold choice](research/scaffold-choice-2026-10.md))
is **retired**: recipes are installed from the catalog / local directories
(`gogogo recipe install`), not chosen at scaffold time. Every non-core unit
now carries a manual-removal `Reason` in the registry — that text is the
installer surface (`gogogo recipe describe` / MCP `capabilities_list`).

## Where the class lives

In the registry, next to the unit's identity:

```go
{ID: "review", Kind: KindFeature, Dirs: []string{...}},
```

The class is one value: `core` = the binary or its shipped gates won't compile
or boot; `plugin` = binary works but loses a capability (presentational name: servant); `feature` = pure demo / recipe. The **directory is
the layer** — `internal/` is infrastructure, `features/` is product — so there
is no separate `layer` axis to keep in sync (a two-axis `layer=…,removal=…` form was considered and rejected for exactly that reason, and so was the per-file header that carried one).

**`internal/` is Go's import boundary, not a removal class.** It keeps a package
out of the public API; it says nothing about whether the app survives without
it. Roughly half the packages under `internal/` are plugins — `nats`, `dagnats`,
`goakt`, `collab`, `search`, `llm`, `datastar`, `components` — each removable
with the capability it serves (its row says so). The other half is
core because core wiring names it: `auth`, `db`, `dbschema`, `queue` (the SSE
Hub), `natswire`, `rbac`, `server`, `secrets`, `observability`. Read the row,
never the directory.

**Enforcement.** The registry is data and its consumers are generated or
checked: a package under `internal/` or `features/` that no row owns fails
`internal/capabilities`, a row whose `Dirs`/`Files` do not exist fails too, and
a unit that mounts itself is reached through the generated
`internal/server/units.gen.go` (`Capability.Register`), so the composition root
never names it. A per-file removal-class header coming back fails the no-legacy
guard — the class is not written twice.

## Rule of thumb for agents

Look the package up in `internal/capabilities` (or ask `gogogo`/`docs/unit-contracts.md`
for the row). **Never delete a `core` row without asking. Never keep a
`feature` row in production if the domain doesn't need it.**

`feature` and `plugin` rows are free to delete: the row names the dirs and files
the unit owns, and a unit that mounts itself (`Register`) has no line in the
composition root to chase. A unit that does not is removed with its wiring call
(`BootChi` in `internal/server/chi.go`, `cmd/web/main.go`).

## Demo-only markup

Some surfaces exist only to advertise the template itself: the
`TechStack` stack key the State/Stack panel renders (the Stack button), and the demo-accounts
block on `/login` (the seeded pair, their roles, and the Casbin
exercise). They teach gogogo's architecture and say nothing about a
product's own domain, so a trimmed product must delete them.

The machine handle is a `data-demo="<name>"` attribute on the element's
wrapper. Two ship: `data-demo="stack-key"` (every `TechStack`) and
`data-demo="demo-accounts"` (the login card). To strip them from a
product build:

```
grep -rn 'data-demo=' features/ internal/ web/
```

Delete each matched element (or the template branch around it). Tests
(`internal/components/techbadge_test.go`,
`features/authviews/login_test.go`) pin the handles, so an accidental
rename fails CI instead of shipping quietly.

## Removing a component

The flat stack composes every capability in two places, both inside
`internal/server/chi.go`: `BootChi` builds each handler, `buildChiMux` flattens
its routes onto the root with one `MountFeature` call. The installer (`gogogo` keep lists / presets)
automated the common ones; when doing it by hand:

1. Delete the package directory (e.g. `features/todo/`).
2. Delete dependent packages listed in the `Depends on:` comment.
3. Drop the handler construction and `r.MountFeature(...)` line from
   `internal/server/chi.go` (and its test fixtures).
4. If it was a plugin, also remove the `start*` call in `cmd/web/main.go`.
5. Run `go build ./cmd/web` (and the registry's tests) to catch anything left behind.

## Graceful degradation (UI rule)

Optional capabilities with UI must degrade through **signals, never 404s**:
the tab/button/hint renders only when a `todo.Signals` bool is true, and
that bool is registration truth (`config enabled && handler wired`), not
config truth. Precedents: `DagNatsEnabled` (tab + container + empty-state hints), `LLMEnabled` (AI tab). When you add a
removable capability with UI: add the signal, gate every page that renders it, record it
as `UISignal` in `internal/capabilities` — the conformance test fails if
the field or a `.templ` reader goes missing.

## What ships as what

The class is written once, as `Kind` in `internal/capabilities`, and the table
below is a reading aid, not a second source: a package's class is whatever the
row that owns it declares, and the registry's coverage test fails when a
package under `internal/` or `features/` has no owning row (it walks
recursively, so a subpackage like `features/chat/handlers` is covered by its
top-level unit's dirs).

| Package | Class | Remove by |
|---|---|---|
| `cmd/web/` | 🔴 CORE | Never — entry point |
| `config/` | 🔴 CORE | Never — env config |
| `internal/db/` | 🔴 CORE | Never — platform driver open (tursogo on desktop/server, ncruces on Android); importing it links the driver |
| `internal/dbschema/` | 🔴 CORE | Never — driver-free migrations + IDs/timestamps; shared with zero-CGO targets |
| `internal/secrets/` | 🔴 CORE | Never — age loader |
| `internal/queue/` | 🔴 CORE | Never — SSE Hub (every streaming handler names `*queue.SSEHub`) + goqite jobs + workers + retry |
| `internal/server/` | 🔴 CORE | Never — flat stack composition (BootChi + buildChiMux) |
| `internal/auth/` | 🔴 CORE | Never — argon2id + sessions (owned auth) |
| `internal/rbac/` | 🔴 CORE | Never — Casbin enforcer wired into the whole mux (`RBAC_ENABLED` decides whether it enforces) |
| `internal/natswire/` | 🔴 CORE | Never — handlers name its types in signatures even with NATS off, so trimming would not compile |
| `internal/demo/` | 🔴 CORE | Never — the fresh-clone way in (one idempotent seed) |
| `internal/observability/` | 🔴 CORE | Never — slog + tracing |
| `internal/hostgate/` | 🔴 CORE | Never — test-capacity gate (tune with `HOSTGATE_*`) |
| `internal/ui/`, `internal/uiregistry/`, `internal/icon/`, `internal/themes/` | 🔴 CORE | Never — one shell, the module registry, the icon set and the DaisyUI theme catalog |
| `internal/instance/` | 🔴 CORE | Never — instance identity used by the runtime |
| `features/app/` | 🟡 PLUGIN | Delete it to drop `cmd/gui`'s dependency bundle; `cmd/web` never links it (handlers take their deps directly) |
| `features/authviews/` | 🔴 CORE | Holds the shared shell controls (theme, user menu, palette) every page renders; the login page itself is manual removal |
| `web/resources/` | 🔴 CORE | Never — embedded static assets |
| `internal/datastar/` | 🟡 PLUGIN | Swap for another implementation, or delete + the wiring call in every page (manual) |
| `internal/components/` | 🟡 PLUGIN | Shared UI (Toast, SessionBanner, OfflineBanner, RealtimeResync) — 14 importers, so removal is manual |
| `internal/nats/` | 🟡 PLUGIN | Delete + wiring call (or `NATS_ENABLED=false`) |
| `internal/dagnats/` | 🟡 PLUGIN | Delete `features/onboarding/` + `cmd/web/dagnats.go` too (or `DAGNATS_ENABLED=false`) |
| `internal/llm/` | 🟡 PLUGIN | Delete + wiring call (or unset `GOAI_API_KEY`) |
| `internal/collab/` | 🟡 PLUGIN | Delete `features/whiteboard/` too |
| `internal/goakt/` | 🟡 PLUGIN | Delete `features/room/` + `cmd/web/goakt.go` too (or `GOAKT_ENABLED=false`) |
| `internal/search/` | 🟡 PLUGIN | Delete the package + the search wiring; the SQL substring layer degrades rather than disappearing (the seam is swappable) |
| `web/resources/static/offline-outbox.js` + `sw.js` + `config/config.go` (offline-sync) | 🟡 PLUGIN | Manual; `OFFLINE_SYNC_ENABLED=false` disables the page outbox and the NATS CRUD proxy together. On the async axis this is a mode, not one of the seven layers |
| `features/review/` (Genkit flows) | 🟡 PLUGIN | The flow runtime behind the Review feature; `go mod tidy` drops Genkit + go-jev when the feature goes |
| `internal/capabilities/`, `internal/installer/` | 🟡 PLUGIN | The installer itself: deleting breaks `cmd/gogogo`, not the web binary |
| `features/sounds/` | 🟡 PLUGIN | Delete the package + its consumers (the checklist lives in `features/sounds/sounds.go`) |
| `features/credits/` | 🟡 PLUGIN | Optional ledger (`CREDITS_ENABLED=false` already the default) |
| `features/onboarding/` | 🟡 PLUGIN | The workflow engine side of the durable run — goes with `internal/dagnats/` |
| `features/genui/` | 🟡 PLUGIN | The rendering capability behind Chat; one bundle with `features/chat/` — delete both |
| `features/todo/` | 🟢 FEATURE | Delete package + wiring call |
| `features/chat/` | 🟢 FEATURE | Bundled into the `genui` unit (chat imports genui components, so both trim together) |
| `features/review/` | 🟢 FEATURE | Delete package (its Register) + regenerate `internal/server/units.gen.go` + `go mod tidy` (drops genkit + go-jev) |
| `features/whiteboard/` | 🟢 FEATURE | Delete package + `internal/collab/` |
| `features/notes/` | 🟢 FEATURE | Manual: delete the dir + its roots in `buildChiMux` + the `[]Module` entry in `internal/ui/modules.go` |
| `features/room/` | 🟢 FEATURE | The room demo page; the engine behind it is the `goakt` plugin (either class is accepted for this dir — both units own it) |
| `features/landing/` | 🟢 FEATURE | Delete package + wiring call |
| `features/config/` | 🟢 FEATURE | Delete package + wiring call |
| `features/account/` | 🟢 FEATURE | Delete package + wiring call |


## The unit registry

The generated [unit contracts](unit-contracts.md) is the canonical per-unit map:
it is rendered from `internal/capabilities` and pins each unit's kind, dirs,
routes, runtime off and trim. The table above is the directory view; a
directory can host more than one unit (for example `features/review/` holds
the `review` feature and the `genkit` plugin).

- **Core:** `appconfig`, `auth`, `crudwire`, `database`, `demo-seed`,
  `hostgate`, `instance`, `observability`, `queue`, `rbac`, `recipe`, `secrets`,
  `server`, `shell` (plus `cloudcfg`, `license` — see registry).
- **Plugin (site: Servant):** `app`, `capabilities`, `collab`, `components`, `credits`,
  `dagnats`, `datastar`, `delivery`, `desktopboot`, `genkit`, `genui`, `goakt`, `graph`, `i18n`, `llm`, `nats`,
  `offline-sync`, `perf`, `search`, `sounds`.
- **Feature (site: Recipe):** `account`, `chat`, `config-view`, `dashboard`, `landing`, `notes`, `notifications`, `review`,
  `room`, `todo`, `whiteboard` (all self-mounting via `Capability.Register`).
  `graph` and `license-activation` are Servants (manual mounts, not installable recipes: the panorama host and the standalone activation page).

## Bundled receipts (ownership, not installed registrations)

Every self-mounting bundled unit carries a receipt in `.recipes/<id>.yml`
(front-matter declaration + pristine hashes of its `features/<id>/` tree).
`gogogo recipe status` reports each one as pristine when the tree matches;
`gogogo recipe uninstall <id> --yes` removes the pristine files, drops its
policy block, regenerates `recipes.gen.go` and `units.gen.go`, and leaves a
building tree. Because the bundled set mounts via `units.gen.go` (registry),
`recipes.gen.go` deliberately skips bundled Register paths: bundling them
there would double-mount and panic at the route-claim gate. `config-view`
is the one bundled unit without a `<id>.yml` receipt: its routes are
admin-only (`p, admin, /config…`), so the user-owned recipe policy block
cannot express them; its declaration is documented in the registry instead.
`graph` is a Servant (the panorama is a wired capability over the registry,
not a copyable slice), and `dashboard` is a recipe by kind but a host by role:
hosts are recipes too (they mount via `Register`). `dashboard` hosts
`dashboard.card` — it knows its numbers from
`ListTodosByOwner` (open/total) and `CountUnreadInApp` (unread inbox), lists one
card per installed recipe (its rail destination), then renders
`SlotRegistry.Fragments(SlotDashboardCard)` for out-of-tree contributions;
`graph` renders the capability/recipe map itself. Pomodoro (`_recipes/pomodoro`:
one table, one page, one write, one sidebar entry, and the five contract tests
a recipe that mounts routes must ship) stays the minimal installable example;
Kiosk is the worked instrumentation example — a closed-menu parser with
on-device voice and one declared outcome (a success/total pair over its own
table, `recipe describe kiosk`). It ships **bundled**, like the other demos:
the code lives in `features/kiosk`, the receipt in `.recipes/kiosk.yml`, and
the registry row is what says it is a feature — so it mounts by default and
`recipe uninstall kiosk` removes it like any other. `features/todo` stays the
in-tree reference slice. New cost: none — deleting a bundled unit is
`recipe uninstall <id>` (see `docs/features.md`).

## Trim vs recipe: what shadcn does and what we do

shadcn's registry is distribution only: `add` copies files and records
deps; there is **no uninstall** (remove = delete files by hand) and no hash
receipt — an update is an overwrite (`--overwrite`), so local edits are
clobbered without a three-way merge. gogogo goes further on purpose: the
receipt stores pristine hashes, the drift check classifies pristine /
modified / deleted, and `update` keeps user edits (three-way via git) while
`uninstall` removes only pristine. That ownership is why bundled receipts
must live in `.recipes/` even for in-tree units.

What gogogo does that a copy-paste library does not — the same ownership
pillar ("traditional libraries create dependency relationships; shadcn/ui
creates ownership relationships"), taken further:

1. **A vertical slice, not a fragment.** shadcn copies one UI component;
   a recipe is a vertical slice — page + handlers + components + tables +
   queue jobs + policy rows: an owned product surface, not files dropped
   in a pile. And the declaration is *applied* by the engine (schema via
   seal, policy rows), not merely shipped: receipt/code drift fails the
   build (no anchors).
2. **Receipt + drift.** shadcn has no hash check and no notion of what
   changed after the copy; `recipe status` answers pristine / modified /
   deleted per file, so "your edit vs upstream's edit" is never a guess,
   and `update` keeps your edits instead of overwriting them.
3. **A lifecycle, not add-only.** `recipe uninstall` (pristine-only),
   `recipe status`, `recipe outdated` (the version verdict, exit 3) and
   `recipe list` / `describe` / `invoke` — discovery and exit under the
   same noun that installs. shadcn's own docs name the cost of ownership
   (manual updates, you're the maintainer): gogogo prices it in. The
   receipt stays when the recipe is removed, because the data still has
   an owner.

Trim is **not** the recipe mechanism: it is scaffold-time removal of
Servants/Core (engines and deps the binary would otherwise keep — `nats`,
`dagnats`, `collab`, `search`, `goakt`, `genui`). For Features, trim
overlaps `recipe uninstall <builtin>`; keeping both confuses, so Features go
via `uninstall` and Trim keeps only Servants. `graph` is a Servant, not a
recipe: it renders the recipe map, so making the panorama itself installable
would be recursive, and it carries no receipt, so `recipe uninstall graph`
could never honor a Feature contract. Servant fits — the binary works without
it and loses the panorama capability — and Trim can remove it at scaffold
time. The rail lists recipes only; Servant/Core surfaces (graph, config,
dashboard-the-host) live in the account menu.

## Cross-package test dependencies

Tests come along with the package you remove, **except** when a test in
another package imports the deleted one. Check these when you trim:

| If you remove… | Delete these packages | Watch for |
|---|---|---|
| **Todo** | `features/todo/` | its own tests come automatically |
| **Whiteboard** | `features/whiteboard/` (including `static/`), `internal/collab/` | its own tests come automatically |
| **DagNats** | `internal/dagnats/`, `features/onboarding/` | ⚠️ `features/todo/handlers/onboarding_test.go` and `internal/capabilities` reference it |
| **NATS** | `internal/nats/` | ⚠️ `internal/collab` may depend on NATS |
| **OfflineSync** | `config/config.go` (`OFFLINE_SYNC_ENABLED`), `sw.js`, `features/todo/handlers/bridge.go` | `internal/natswire/wire_test.go` + `features/todo/handlers/bridge_test.go` cover create/toggle/delete/clear_completed e2e with JetStream |
| **LLM** | `internal/llm/` | ⚠️ `features/todo/handlers/suggest_test.go` |
| **Turso queries** | (only layer) | There is no strategy switch anymore: handlers call `dbgen.*` directly |
| **Idempotency** | `internal/dbschema/migrations/0001_init.sql` (`todos_idem_key_unique`) + `features/todo/handlers/mutations.go` | Drop the partial unique index and the `GetTodoByIdemKey` dedupe branch, then the `idem_key` query param from `todo_list.templ` |
| **Sounds** | `features/sounds/`, `web/resources/static/cuelume.js`, `web/resources/static/cuelume/` | Drop `@sounds.SoundAssets()` from page layouts and `@sounds.SoundSwitch()` from each page's `*Sound()` wrapper. |

**After any removal, run `make test`** (not a bare `go test ./...` — that
compiles `cmd/desktop`/`cmd/gui`/`cmd/mobile`, which need GTK + WebKit or the Android SDK; see
[desktop & mobile](desktop-mobile.md)). If a compilation error mentions the
deleted package in a test file, delete that test file too. Cross-package tests
that import a trimmed package are the usual culprit; `make check-flatdeps` and
`gogogo --check` catch the wiring half before you build.

## Related

- [Architecture](architecture.md)
- [Features](features.md)
- [Native boundary](exception-to-go.md) — why `native` is an implementation boundary, not a fourth layer.