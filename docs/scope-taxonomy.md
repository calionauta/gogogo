# Scope taxonomy

Every non-test, non-generated `.go` file under `internal/` and `features/`
carries a `SCOPE` annotation in its leading doc comment to tell agents and
developers what can be safely removed. (Files elsewhere — `cmd/`, `web/`,
`config/` — have none; the linter only walks `internal/` and
`features/`, skipping `*_test.go` and `*_templ.go`.) This is the rule that makes
the template safely trimmable: you never have to guess whether a directory is
load-bearing.

## The three removal classes

The annotation carries a single axis — the removal class — with three values:

| Class | Meaning | You would… |
|------------|---------|-----------|
| `core` 🔴 | The web binary **or its gates** do not work without it. Some have a runtime opt-out via env vars. | Customize, never remove. |
| `plugin` 🟡 | Binary works but loses a capability. A plugin serves other capabilities (jobs, metering, sounds) — no page disappears, but something stops working. | Swap for another implementation, or delete the package + wiring call (e.g. `BootChi` in `internal/server/chi.go`, `cmd/web/main.go`). |
| `feature` 🟢 | A demo/add-on. A feature is a terminal user surface — a page or journey disappears (todo, whiteboard, landing, config). | Keep as a reference while building your own, then remove. |

Three clarifications the table alone does not carry:

- **Core includes gate support.** `internal/hostgate` never links into the web
  binary, but every heavy suite and `ci-local` depends on it, so deleting it
  breaks the gates that ship the binary. That is the "or its gates" clause.
- **Plugin does not promise a one-line delete.** `internal/nats`, `internal/llm`
  and `internal/collab` are named in signatures, so removing one is a manual
  refactor that touches its consumers; the class answers *what disappears*, not
  *how mechanical the trim is*. `SCOPE`-core units are the ones where removal
  is not coherent at all.
- **The servant wins ties.** A unit that both powers other surfaces and ships a
  page is a plugin when the capability is the servant and the page is its demo.
  `features/genui` is the precedent: the Ask AI/Chat pages disappear with it,
  but the unit exists to render answers for chat, todo panels and review, so it
  is classified plugin (and bundled with `features/chat` for trimming).

## The annotation form

Non-test, non-generated `.go` files under `internal/` and `features/` declare
their removal class in a leading doc comment:

```go
// SCOPE:<core|plugin|feature> — <short description>
```

The class is one value: `core` = the binary or its shipped gates won't compile
or boot; `plugin` = binary works but loses a capability; `feature` = pure demo. The **directory is
the layer** — `internal/` is infrastructure, `features/` is product — so there
is no separate `layer` axis to keep in sync (a two-axis `layer=…,removal=…` form was considered and rejected for exactly that reason).

**`internal/` is Go's import boundary, not a removal class.** It keeps a package
out of the public API; it says nothing about whether the app survives without
it. Roughly half the packages under `internal/` are plugins — `nats`, `dagnats`,
`goakt`, `collab`, `search`, `llm`, `datastar`, `components` — each removable
with the capability it serves (and each `SCOPE` line says so). The other half is
core because core wiring names it: `auth`, `db`, `dbschema`, `queue` (the SSE
Hub), `natswire`, `rbac`, `server`, `secrets`, `observability`. Read the
annotation, never the directory.

**Enforcement.** The `cmd/check-scope` Go program walks every `.go` file in
`internal/` and `features/` and asserts the canonical SCOPE line is present in
the leading doc-comment group. **The single-axis form above is the only
accepted dialect** — the two-axis `layer=…,removal=…` form is rejected,
so the annotation is either canonical or an offender (no drift between
dialects). `make ci-local` runs it via the `check-scope` target, and the
lefthook pre-commit runs it whenever staged files match `{internal,features}/**/*.go`.
Migrating an existing file uses `python3 scripts/migrate-scope.py` (idempotent).

## Rule of thumb for agents

If you see a `SCOPE` annotation on a file, respect it. **Never delete a
`core` file without asking. Never keep a `feature` file in production if the
domain doesn't need it.**

`feature` and `plugin` files are free to delete, after reading the inline
description — it lists exactly what to remove in `internal/server/chi.go`
(the flat route composition) and `cmd/web/`.

## Demo-only markup

Some surfaces exist only to advertise the template itself: the
`TechStack` stack key every feature's "How it works" panel renders, and the demo-accounts
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
its routes onto the root with one `MountFeature` call. The installer (`gogogo` keep lists / presets, plus `gogogo add <unit>` to
restore one) automates the common ones; when doing it by hand:

1. Delete the package directory (e.g. `features/todo/`).
2. Delete dependent packages listed in the `Depends on:` comment.
3. Drop the handler construction and `r.MountFeature(...)` line from
   `internal/server/chi.go` (and its test fixtures).
4. If it was a plugin, also remove the `start*` call in `cmd/web/main.go`.
5. Run `make check-scope && go build ./cmd/web` to catch anything left behind.

## Graceful degradation (UI rule)

Optional capabilities with UI must degrade through **signals, never 404s**:
the tab/button/hint renders only when a `todo.Signals` bool is true, and
that bool is registration truth (`config enabled && handler wired`), not
config truth. Precedents: `DagNatsEnabled` (tab + container + empty-state hints), `LLMEnabled` (AI tab). When you add a
removable capability with UI: add the signal, gate every page that renders it, record it
as `UISignal` in `internal/capabilities` — the conformance test fails if
the field or a `.templ` reader goes missing.

## What ships as what

The class is written twice — in the file's `SCOPE` line and as `Kind` in
`internal/capabilities`. **The registry is the source of truth for kinds**;
a file must not disagree with the unit that owns it, and
`TestScopeAnnotationsMatchRegistryKind` fails the build when one does (it walks
recursively, so a subpackage like `features/chat/handlers` is checked against
its top-level unit too).

| Package | Class | Remove by |
|---|---|---|
| `cmd/web/` | 🔴 CORE | Never — entry point |
| `config/` | 🔴 CORE | Never — env config |
| `internal/db/` | 🔴 CORE | Never — Turso connection open (tursogo); importing it links the driver |
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
| `internal/capabilities/`, `internal/installer/` | 🟡 PLUGIN | The installer itself: deleting breaks `cmd/gogogo`, not the web binary |
| `features/sounds/` | 🟡 PLUGIN | Full checklist in the `SCOPE` doc comment in `features/sounds/sounds.go` |
| `features/credits/` | 🟡 PLUGIN | Optional ledger (`CREDITS_ENABLED=false` already the default) |
| `features/onboarding/` | 🟡 PLUGIN | The workflow engine side of the durable run — goes with `internal/dagnats/` |
| `features/genui/` | 🟡 PLUGIN | The rendering capability behind Chat; one bundle with `features/chat/` — delete both + `internal/server/genui_stubs_test.go` |
| `features/todo/` | 🟢 FEATURE | Delete package + wiring call |
| `features/chat/` | 🟢 FEATURE | Bundled into the `genui` unit (chat imports genui components, so both trim together) |
| `features/review/` | 🟢 FEATURE | Delete package + `bootReview`/`reviewH` lines + `/review` mounts in `internal/server/chi.go` (+ `newReviewHandlerForTest` in its test) + `go mod tidy` (drops genkit + go-jev) |
| `features/whiteboard/` | 🟢 FEATURE | Delete package + `internal/collab/` |
| `features/notes/` | 🟢 FEATURE | Manual: delete the dir + its roots in `buildChiMux` + the `[]Module` entry in `internal/ui/modules.go` |
| `features/room/` | 🟢 FEATURE | The room demo page; the engine behind it is the `goakt` plugin (either class is accepted for this dir — both units own it) |
| `features/landing/` | 🟢 FEATURE | Delete package + wiring call |
| `features/config/` | 🟢 FEATURE | Delete package + wiring call |

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
| **Sounds** | `features/sounds/`, `web/resources/static/cuelume.js`, `web/resources/static/cuelume/` | Drop `@sounds.SoundAssets()` from page layouts and `@sounds.SoundToggle()` / `@sounds.SoundSwitch()` from each page's `*Sound()` wrapper.ActiveAssets` in the shell |

**After any removal, run `make test`** (not a bare `go test ./...` — that
compiles `cmd/desktop`/`cmd/gui`, which need GTK + WebKit headers; see
[desktop & mobile](desktop-mobile.md)). If a compilation error mentions the
deleted package in a test file, delete that test file too. Cross-package tests
that import a trimmed package are the usual culprit; `make check-flatdeps` and
`gogogo --check` catch the wiring half before you build.

## Related

- [Architecture](architecture.md)
- [Features](features.md)
- [Native boundary](exception-to-go.md) — why `native` is an implementation boundary, not a fourth layer.