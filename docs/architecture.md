# Architecture

This page is the map of how the pieces fit. For the per-component table with
dependencies and removal instructions, see [ARCHITECTURE.md](../ARCHITECTURE.md)
in the repo root — that file is the canonical entrypoint for LLM agents and is
kept current with the code.

## Principles

- **Unified build.** One `go build ./cmd/web` compiles everything — no build
  tags. `ncruces/go-sqlite3` is the always-on SQLite driver (it registers
  `sqlite3`); PocketBase also bundles `modernc.org/sqlite`, but that registers
  `sqlite` and stays unused, so nothing is gated behind a tag. Opt out at
  runtime with env vars (`NATS_ENABLED=false`, `DAGNATS_ENABLED=false`).
- **Features in `features/`, infrastructure in `internal/`.** Features depend
  on infra, never the reverse.
- **Wiring in `router/router.go` → `Init()`.** Every feature is registered by
  one function call.
- **Startup order in `cmd/web/main.go`.**

## Directory layout

```
cmd/web/                          Entry point (PB + goqite + SSE Hub + DagNats + NATS)
cmd/desktop/                      Wails v3 desktop/edge shell (NATS Leaf Node when set)
cmd/gui/                          gogpu/ui native window PoC over the same backend
config/                           Env config (+ age secrets)
db/                               PocketBase setup + seed
internal/
  secrets/                        age-decrypted secrets loader
  queue/                          goqite + SSE Hub + workers + retry + handler registry
  datastar/                       Datastar SSE rendering helpers
  nats/                           NATS JetStream + embedded server
  dagnats/                        DagNats durable workflow client
  goakt/                          GoAkt entity actors (room authority)
  llm/                            GoAI LLM SDK helpers
  collab/                         Loro CRDT + DocStore + sync workers + presence
features/
  app/                            AppContext (cross-cutting deps bundle)
  auth/                           Login/logout/cookie (UI) + middleware
  store/                          EntityStore interface (PB + CRDT strategies)
  landing/                        Public marketing hero on GET /
  config/                         Auth-gated read-only /config view
  todo/                           Todo MVC example
  whiteboard/                     Collaborative canvas
  room/                           Room presence demo (GoAkt grains)
  notes/                          Shared plain-text notes (server-owned Loro Text)
  sounds/                         UI sound feedback (cuelume)
web/
  resources/                      Embedded static assets
  skins/                          Pluggable UI skin registry
router/                           Route wiring (central dependency graph)
```

The annotated version of this tree, with SCOPE markers on every entry, is in
[Scope taxonomy](scope-taxonomy.md).

## Dependency direction

```
cmd/web  ──▶  router  ──▶  features/*  ──▶  internal/*  ──▶  (stdlib, deps)
              ▲              │
              └──────────────┘
         features depend on internal, never the reverse
```

`router/router.go` is the single place where the dependency graph is
assembled. If you are adding a feature, that is the only file you touch for
wiring — the feature's own `RegisterRoutes` does the rest.

## The three app entry points

One backend, three frontends:

| Entry point | Transport | Purpose |
|---|---|---|
| `cmd/web` | HTTP + SSE | The main server: PocketBase, router, queue, realtime |
| `cmd/desktop` | Wails v3 webview over a local HTTP proxy | Native desktop/edge shell, 100% shared backend |
| `cmd/gui` | gogpu/ui native window | Second frontend with **no** HTTP at all — proves the domain layer is transport-agnostic |

`cmd/desktop` and `cmd/gui` are separate build targets excluded from the web
CI. See [Desktop & Mobile](desktop-mobile.md).

## Route wiring gotcha

PocketBase's `RouterGroup` compiles to the stdlib `http.ServeMux` (Go 1.22+
subtree matching — `GET /` swallows unregistered subpaths). Therefore:

- Register all routes **directly** on `se.Router` inside the `OnServe` hook.
  A nested `OnServe().BindFunc` never fires.
- Serve static assets via **exact** `/static/<file>` routes; the PB catch-all
  shadows wildcards.
- The app cookie is `gogogo_auth`, **not** `pb_auth` — see
  [Admin & Dashboard](admin-dashboard.md#app-session-cookie-vs-pb_auth-why-two).

## SCOPE annotations

Every non-test, non-generated `.go` file under `internal/` and `features/`
carries a leading doc-comment line declaring its SCOPE on two orthogonal axes:

```
// SCOPE:layer=<infra|feature>,removal=<core|plugin|feature> — <short description>
```

The `cmd/check-scope` program walks every `.go` file in `internal/` and
`features/` and asserts the canonical line is present. `make ci-local` runs it,
and the lefthook pre-commit runs it whenever staged files match
`{internal,features}/**/*.go`. Migrating an existing file uses
`python3 scripts/migrate-scope.py` (idempotent).

See [Scope taxonomy](scope-taxonomy.md) for the full rules.

> **Native boundary (not a layer).** `native` is a reserved implementation
> boundary, not a fourth SCOPE value. There is no Zig code or toolchain in
> this repo. If a justified case ever arrives, the Go API lives in its normal
> package with the usual SCOPE and Zig sources stay inside it behind a small
> C ABI — see [Native boundary](exception-to-go.md). Do not create a top-level
> `native/` directory speculatively.

## Related

- [Stack in layers, not silos](stack-layers.md) — what each dependency is for.
- [Seven async layers](async-layers.md) — the realtime and async topology.
- [Configuration](configuration.md) — where constants live and why.
- [Native boundary](exception-to-go.md) — Go-first policy and the Zig escape hatch.