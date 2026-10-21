# Architecture

This page is the map of how the pieces fit. For the per-component table with
dependencies and removal instructions, see [ARCHITECTURE.md](../ARCHITECTURE.md)
in the repo root — that file is the canonical entrypoint for LLM agents and is
kept current with the code.

## Principles

- **Unified build.** One `go build ./cmd/web` compiles everything — no build
  tags. `turso` (tursogo) is the always-on Turso driver, registered by
  `internal/db`; handlers call the generated `dbgen.*` queries directly, so
  nothing is gated behind a tag. Opt out at runtime with env vars
  (`NATS_ENABLED=false`, `DAGNATS_ENABLED=false`).
- **Features in `features/`, infrastructure in `internal/`.** Features depend
  on infra, never the reverse.
- **Wiring in `internal/server/chi.go`.** `BootChi` builds one handler per
  capability; `buildChiMux` flattens each capability's own router onto one
  chi router.
- **Startup order in `cmd/web/main.go`.**

## Directory layout

```
cmd/web/                          Entry point (flat stack + goqite + SSE Hub + DagNats + NATS)
cmd/desktop/                      Wails v3 desktop/edge shell (NATS Leaf Node when set)
cmd/gui/                          gogpu/ui native window over the same backend
config/                           Env config (+ age secrets)
internal/
  demo/                           Fresh-install demo account seed (idempotent Signup)
  secrets/                        age-decrypted secrets loader
  queue/                          goqite + SSE Hub + workers + retry + handler registry
  datastar/                       Datastar SSE rendering helpers
  nats/                           NATS JetStream + embedded server
  dagnats/                        DagNats durable workflow client
  goakt/                          GoAkt entity actors (room authority)
  llm/                            GoAI LLM SDK helpers
  collab/                         Loro CRDT + DocStore + sync workers + presence
  auth/                           Owned argon2id + sessions (gogogo_auth cookie)
  db/                             Turso connection + migrations + sqlc queries
  demo/                           Fresh-install demo account seed
  server/                         Flat-stack composition (BootChi + buildChiMux)
features/
  app/                            AppContext bundle (consumed by cmd/gui only)
  authviews/                      Login/navbar/theme components
  landing/                        Public marketing hero on GET /
  config/                         Auth-gated read-only /config view
  todo/                           Todo MVC example
  whiteboard/                     Collaborative canvas
  room/                           Room presence demo (GoAkt grains)
  notes/                          Shared plain-text notes (server-owned Loro Text)
  sounds/                         UI sound feedback (cuelume)
web/
  resources/                      Embedded static assets (AssetHandler: /static/ + root /sw.js)
  skins/                          Pluggable UI skin registry
```

The annotated version of this tree, with SCOPE markers on every entry, is in
[Scope taxonomy](scope-taxonomy.md).

## Dependency direction

```
cmd/web  ──▶  internal/server  ──▶  features/*/chi  ──▶  internal/*  ──▶  (chi, stdlib, deps)
                       │                    │
                       └────────────────────┘
         features depend on internal, never the reverse
```

`internal/server/chi.go` is the single place where the dependency graph is
assembled. If you are adding a feature, that is the only file you touch for
wiring — the feature's own `Routes()` does the rest.

## The three app entry points

One backend, three frontends:

| Entry point | Transport | Purpose |
|---|---|---|
| `cmd/web` | HTTP + SSE | The main server: router, queue, realtime |
| `cmd/desktop` | Wails v3 webview over a local HTTP proxy | Native desktop/edge shell, 100% shared backend |
| `cmd/gui` | gogpu/ui native window | Second frontend with **no** HTTP at all — proves the domain layer is transport-agnostic |

`cmd/desktop` and `cmd/gui` are separate build targets excluded from the web
CI. See [Desktop & Mobile](desktop-mobile.md).

## Route wiring

The flat stack composes one `chi.Router` (`buildChiMux` in
`internal/server/chi.go`). Each capability exposes `Routes() chi.Router` with
absolute patterns, and `MountFeature` flattens that table onto the root
(`internal/server/route.go`). Therefore:

- One `MountFeature` call per capability. A route added to a feature is
  reachable without an edit at the root, and a duplicate method+pattern is a
  boot panic that names both owners.
- The auth API and the static/asset handlers mount through `MountSubtree`,
  which leaves the request path unrewritten because those handlers trim their
  own prefix. The landing router is the not-found handler.
- Serve static assets through `web/resources.AssetHandler` (`/static/` plus
  root `/sw.js` with `Service-Worker-Allowed: /`).
- The app cookie is `gogogo_auth` — the only cookie — see
  [Admin & Dashboard](admin-dashboard.md#app-session-cookie-why-one).

## SCOPE annotations

Every non-test, non-generated `.go` file under `internal/` and `features/`
carries a leading doc-comment line declaring its removal class:

```
// SCOPE:<core|plugin|feature> — <short description>
```

The directory is the layer (`internal/` = infra, `features/` = product); the
annotation only records what happens if the file is deleted. See
[scope-taxonomy](scope-taxonomy.md).

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