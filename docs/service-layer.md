# Service Layer (transport-free)

> One rule per frontend fact. HTTP, Datastar, native window, offline
> replay and the MyGo shell all call the same `Service`.

## Convention for a new recipe with writes

```
features/<name>/
  service/service.go   # transport-free domain (Q, Index, DB, Bus, hooks)
    ListVisible/ListOwned
    Create (idempotent, indexed, onboarding/Bus publish, hooks)
    Toggle (fresh-read + applyOnce, never snapshot)
    Delete (canDeleteRow), ClearCompleted
    ToTodo/ToVisible*/CanDeleteRow/ValidIdemKey  — single home
  handlers/            # thin: form → svc.Create/Toggle/Delete → SSE patch/toast
  migrations/ recipe.go
```

Rules:

- **No `*http.Request`, no Datastar, no chi in `service`.** If it needs a
  form/header, the handler extracts it and passes a plain string.
- **No `dbgen.Create*` in `handlers`** — indexed writes live in `service`
  (`createIndexed/deleteIndexed/clearIndexed`). The bridge + tests call
  `service.CreateIndexed` wrappers.
- **Exactly-once stays in service:** `applyOnce(ctx, owner, key, work)`.
  Never read-then-flip from a frontend snapshot.
- **Fan-out in service:** `Bus.Publish(Event)` before any "replayed"
  short-circuit. Native windows subscribe in-process via `Bus.Subscribe`,
  no HTTP SSE + cookie + poll fallback needed when embedded.
- **Hooks in service:** onboarding resume, indexer, wire — nil means
  "skip", never "fail the mutation".

## Enforcement

- `go run ./cmd/check-flatdeps` already bans cross-feature domain imports;
  it now also fails when a feature with writes has no `service` package
  (see `serviceLayerFeatures` in `cmd/check-flatdeps/main.go`).
- `desktopboot` (`internal/desktopboot`) is the shared boot for every
  native shell (Wails `cmd/desktop`, gogpu/ui `cmd/gui`, MyGo `cmd/mygo`):
  `MigrateAll`, `NewAuth` (TOTP seal-key), `EnsureDemo`,
  `ScopeInstanceIdentity`/`SingleInstanceID`, `PortOf`, `WebviewURL`.
  Shells never copy the boot sequence.

## Current state

| feature | service | coverage |
|---------|---------|----------|
| `todo` | `features/todo/service` | Create/Toggle/Delete/List/ClearCompleted, onboarding, Bus, applyOnce, tests `-race` |
| `credits` | `features/credits` (lib `ai-credits`) | already service-shaped (`credits.Service`) — handlers are adapters |
| `chat` | `features/chat/store` | store-shaped, adapters in handlers |
| `notes` | TODO | `ListDocs` duplicated in `cmd/gui/store.go` — next extraction |
| `whiteboard` | TODO | same as notes (shared collaborative docs) |
| `notifications` | TODO | inbox + `remind` + Bus — same shape as todo |
| `room` | TODO | presence + patches — wrap transport, keep service core |
| `graph`/`review`/`config`/`license`/`account`/`dashboard` | TODO | follow the same skeleton when a second frontend (native/desktop) appears |

Shipping a recipe without a service keeps it web-only; adding a native
frontend **requires** the service first, or the duplication this work
removed (guiStore copy, TOTP copy, onboarding skip, publish bypass,
toggle divergence) comes back.

## MyGo note

`cmd/mygo` boots through `desktopboot` and points its webview at
`WebviewURL()` (loopback Datastar/Templ, no `frontendDist`/Vite/Bun
needed) — the shipped v1 shell. The native follow-up opens
`ui.View(serviceView)` calling the same `Service` directly (no HTTP, no
cookie, no Vite SPA required) over `ncruces` + `desktopboot`; `Bind` is
the optional RPC path, not the core path. Evidence and build recipe:
[MyGo: the spike and the shell](research/mygo-2026-11.md).
