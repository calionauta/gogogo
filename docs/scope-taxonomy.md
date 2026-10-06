# Scope taxonomy

Every non-test, non-generated `.go` file under `internal/` and `features/`
carries a `SCOPE` annotation in its leading doc comment to tell agents and
developers what can be safely removed. (Files elsewhere — `cmd/`, `db/`,
`web/`, `config/`, `router/` — have none; the linter only walks `internal/` and
`features/`, skipping `*_test.go` and `*_templ.go`.) This is the rule that makes
the template safely trimmable: you never have to guess whether a directory is
load-bearing.

## The three layers

| Annotation | Meaning | You would… |
|------------|---------|-----------|
| `SCOPE:core` 🔴 | Binary does not work without it. Some have a runtime opt-out via env vars. | Customize, never remove. |
| `SCOPE:plugin` 🟡 | Binary works but loses a capability. A plugin serves other capabilities (jobs, metering, sounds, skins) — no page disappears, but something stops working. | Swap for another implementation, or delete the package + wiring call (e.g. `router.Init`, `cmd/web/main.go`). |
| `SCOPE:feature` 🟢 | A demo/add-on. A feature is a terminal user surface — a page or journey disappears (todo, whiteboard, landing, config). | Keep as a reference while building your own, then remove. |

## The two-axis annotation form

Non-test, non-generated `.go` files under `internal/` and `features/` declare
their SCOPE on two **orthogonal** axes in a leading doc comment:

```go
// SCOPE:layer=<infra|feature>,removal=<core|plugin|feature> — <short description>
```

| Axis | Values | Means |
|------|--------|-------|
| `layer` | `infra` · `feature` | Where it lives / what kind of code. `infra` for cross-cutting plumbing in `internal/`; `feature` for product-level code in `features/`. Some packages in `features/` (e.g. `features/auth`, `features/store`) carry `infra` semantics for individual files. |
| `removal` | `core` · `plugin` · `feature` | What happens if you delete it. `core` = binary won't compile or won't boot; `plugin` = binary works but loses a capability; `feature` = pure demo. |

The two-axis scheme eliminates a prior ambiguity where `SCOPE:core - REMOVE if
not using NATS` and `SCOPE:core - DO NOT REMOVE - SSE Hub` shared the same
label but meant different things. With the new axes, the first becomes
`layer=infra,removal=plugin` (binary works without it) and the second stays
`layer=infra,removal=core` (binary breaks without it).

**Enforcement.** The `cmd/check-scope` Go program walks every `.go` file in
`internal/` and `features/` and asserts the canonical SCOPE line is present in
the leading doc-comment group. `make ci-local` runs it via the `check-scope`
target, and the lefthook pre-commit runs it whenever staged files match
`{internal,features}/**/*.go`. Migrating an existing file uses
`python3 scripts/migrate-scope.py` (idempotent).

## Rule of thumb for agents

If you see a `SCOPE` annotation on a file, respect it. **Never delete a
`removal=core` file without asking. Never keep a `removal=feature` file in
production if the domain doesn't need it.**

`removal=feature` and `removal=plugin` files are free to delete, after reading
the inline description — it lists exactly what to remove in `router/router.go`
and `cmd/web/`.

## Removing a component

1. Delete the package directory (e.g. `features/todo/`).
2. Delete dependent packages listed in the `Depends on:` comment.
3. Remove the wiring call from `router/router.go` → `Init()`.
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
| `db/` | 🔴 CORE | Never — PocketBase setup + seed |
| `internal/secrets/` | 🔴 CORE | Never — age loader |
| `internal/queue/` | 🔴 CORE | Never — goqite + SSE Hub + workers + retry |
| `internal/datastar/` | 🟡 PLUGIN | Delete + wiring call |
| `internal/nats/` | 🟡 PLUGIN | Delete + wiring call (or `NATS_ENABLED=false`) |
| `internal/dagnats/` | 🟡 PLUGIN | Delete `router/onboarding_dagnats.go` too (or `DAGNATS_ENABLED=false`) |
| `internal/llm/` | 🟡 PLUGIN | Delete + wiring call |
| `internal/collab/` | 🟡 PLUGIN | Delete `features/whiteboard/` too |
| `internal/components/` | 🟡 PLUGIN | Shared UI helpers (Toast + OfflineBanner) |
| `features/store/pbstore/` | 🟡 PLUGIN | Drop `todoH.SetStore(pbstore.New(app, "todos"))` from `router.Init`; the handler's lazy fallback rebuilds a PBStore on first use |
| `features/app/` | 🔴 CORE | AppContext (cross-cutting deps bundle) |
| `features/auth/` | 🔴 CORE (middleware) / 🟢 FEATURE (UI) | Keep middleware; UI removable |
| `web/skins/` | 🟡 PLUGIN | Delete + drop blank imports in `features/todo/components/skin_imports.go` + drop `SkinSelector` from navbar |
| `features/sounds/` | 🟡 PLUGIN | Full checklist in the `SCOPE` doc comment in `features/sounds/sounds.go` |
| `features/todo/` | 🟢 FEATURE | Delete package + wiring call |
| `features/whiteboard/` | 🟢 FEATURE | Delete package + `internal/collab/` |
| `features/landing/` | 🟢 FEATURE | Delete package + wiring call |
| `features/config/` | 🟢 FEATURE | Delete package + wiring call |
| `router/onboarding_dagnats.go` | 🟢 FEATURE | Delete with `internal/dagnats/` |
| `router/realtime_jet.go` | 🟢 FEATURE | Delete with `internal/nats/` |
| `web/resources/` | 🔴 CORE | Never — embedded static assets |
| `router/` | 🔴 CORE | Never — route wiring |

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
| **EntityStore** | `features/store/pbstore/` | Drop `SetStore(pbstore.New(...))` from `router.Init` |
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
- [Native boundary](native-zig.md) — why `native` is an implementation boundary, not a fourth layer.