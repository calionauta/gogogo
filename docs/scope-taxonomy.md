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
| `core` 🔴 | Binary does not work without it. Some have a runtime opt-out via env vars. | Customize, never remove. |
| `plugin` 🟡 | Binary works but loses a capability. A plugin serves other capabilities (jobs, metering, sounds, skins) — no page disappears, but something stops working. | Swap for another implementation, or delete the package + wiring call (e.g. `BootChi` in `internal/server/chi.go`, `cmd/web/main.go`). |
| `feature` 🟢 | A demo/add-on. A feature is a terminal user surface — a page or journey disappears (todo, whiteboard, landing, config). | Keep as a reference while building your own, then remove. |

## The annotation form

Non-test, non-generated `.go` files under `internal/` and `features/` declare
their removal class in a leading doc comment:

```go
// SCOPE:<core|plugin|feature> — <short description>
```

The class is one value: `core` = binary won't compile or won't boot; `plugin` =
binary works but loses a capability; `feature` = pure demo. The **directory is
the layer** — `internal/` is infrastructure, `features/` is product — so there
is no separate `layer` axis to keep in sync (a two-axis `layer=…,removal=…` form was considered and rejected for exactly that reason).

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

## Removing a component

The flat stack composes every capability in two places, both inside
`internal/server/chi.go`: `BootChi` builds each handler, `buildChiMux`
mounts its routes. The installer (`gogogo trim <unit>`) automates the
common ones; when doing it by hand:

1. Delete the package directory (e.g. `features/todo/`).
2. Delete dependent packages listed in the `Depends on:` comment.
3. Drop the handler construction + `mux.Handle(...)` lines from
   `internal/server/chi.go` (and its test fixtures).
4. If it was a plugin, also remove the `start*` call in `cmd/web/main.go`.
5. Run `make check-scope && go build ./cmd/web` to catch anything left behind.

## Graceful degradation (UI rule)

Optional capabilities with UI must degrade through **signals, never 404s**:
the tab/button/hint renders only when a `todo.Signals` bool is true, and
that bool is registration truth (`config enabled && handler wired`), not
config truth. Precedents: `DagNatsEnabled` (tab + container + empty-state
hints across every skin), `LLMEnabled` (AI tab). When you add a
removable capability with UI: add the signal, gate every skin, record it
as `UISignal` in `internal/capabilities` — the conformance test fails if
the field or a `.templ` reader goes missing.

## What ships as what

| Package | SCOPE | Remove by |
|---|---|---|
| `cmd/web/` | 🔴 CORE | Never — entry point |
| `config/` | 🔴 CORE | Never — env config |
| `internal/db/` | 🔴 CORE | Never — Turso connection open (tursogo); importing it links the driver |
| `internal/dbschema/` | 🔴 CORE | Never — driver-free migrations + IDs/timestamps; shared with zero-CGO targets |
| `internal/secrets/` | 🔴 CORE | Never — age loader |
| `internal/queue/` | 🔴 CORE | Never — goqite + SSE Hub + workers + retry |
| `internal/datastar/` | 🟡 PLUGIN | Delete + wiring call |
| `internal/nats/` | 🟡 PLUGIN | Delete + wiring call (or `NATS_ENABLED=false`) |
| `internal/dagnats/` | 🟡 PLUGIN | Delete `features/todo/chi/onboarding.go` + `cmd/web/dagnats.go` too (or `DAGNATS_ENABLED=false`) |
| `internal/llm/` | 🟡 PLUGIN | Delete + wiring call |
| `internal/collab/` | 🟡 PLUGIN | Delete `features/whiteboard/` too |
| `internal/components/` | 🟡 PLUGIN | Shared UI helpers (Toast + OfflineBanner) |
| `internal/server/` | 🔴 CORE | Never — flat stack composition (BootChi + buildChiMux) |
| `internal/auth/` | 🔴 CORE | Never — argon2id + sessions (owned auth) |
| `features/authviews/` | 🟢 FEATURE | Login/navbar/theme components (manual removal) |
| `web/skins/` | 🟡 PLUGIN | Delete + drop blank imports in `features/todo/components/skin_imports.go` + drop `SkinSelector` from navbar |
| `features/sounds/` | 🟡 PLUGIN | Full checklist in the `SCOPE` doc comment in `features/sounds/sounds.go` |
| `features/todo/` | 🟢 FEATURE | Delete package + wiring call |
| `features/whiteboard/` | 🟢 FEATURE | Delete package + `internal/collab/` |
| `features/landing/` | 🟢 FEATURE | Delete package + wiring call |
| `features/config/` | 🟢 FEATURE | Delete package + wiring call |
| `web/resources/` | 🔴 CORE | Never — embedded static assets |

## Cross-package test dependencies

Tests come along with the package you remove, **except** when a test in
another package imports the deleted one. Check these when you trim:

| If you remove… | Delete these packages | Watch for |
|---|---|---|
| **Todo** | `features/todo/` | its own tests come automatically |
| **Whiteboard** | `features/whiteboard/` (including `static/`), `internal/collab/` | its own tests come automatically |
| **DagNats** | `internal/dagnats/`, `router/onboarding_dagnats.go` | ⚠️ `features/todo/onboarding_e2e_test.go` depends on it |
| **NATS** | `internal/nats/` | ⚠️ `internal/collab` may depend on NATS |
| **OfflineSync** | `config/config.go` (`OFFLINE_SYNC_ENABLED`), `sw.js`, `crudproxy.go` | `internal/nats/crudproxy_test.go` covers create/toggle/delete/clear_completed e2e with JetStream |
| **LLM** | `internal/llm/` | ⚠️ `features/todo/suggest_test.go` |
| **Turso queries** | (only layer) | There is no strategy switch anymore: handlers call `dbgen.*` directly |
| **Idempotency** | `db/idempotency_hook.go` + `db/idempotency_seed.go` | Remove `RegisterIdempotencyHook(app)` + `enableTodosIdempotency(col)` from `db/seed.go`, and the hidden `name="idem_key"` input from `createForm` |
| **Sounds** | `features/sounds/`, `web/resources/static/cuelume.js`, `web/resources/static/cuelume/` | Drop `@sounds.SoundAssets()` from page layouts and `@sounds.SoundToggle()` from the navbar |
| **Skins** | `web/skins/` | Drop blank imports + the `SkinSelector` navbar call |

**After any removal, run `go test ./...`.** If a compilation error mentions the
deleted package in a test file, delete that test file too. Cross-package tests
like `features/todo/onboarding_e2e_test.go` depending on `internal/dagnats` are
the usual culprit.

## Related

- [Architecture](architecture.md)
- [Features](features.md)
- [Native boundary](exception-to-go.md) — why `native` is an implementation boundary, not a fourth layer.