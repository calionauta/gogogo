# Desktop & Mobile

The same Go backend (PocketBase + queue + router + handlers) runs as a
**native desktop app** via Wails v3. The desktop build reuses 100% of the
business logic — it boots `internal/server.Run`, serves PocketBase in a
goroutine, and points the webview at it through a reverse proxy.

It is a **separate build target**, not part of the default web build/test loop.
It pulls in Wails v3, which requires GTK + WebKit dev libs
(`libgtk-4-dev libwebkitgtk-6.0-dev`) that only exist on desktop build hosts —
so `make build`, `make test`, `go build ./...` and `go test ./...` from the
repo root exclude it (same exclusion CI applies).

## Build commands

```bash
# Current platform
wails3 build
# Cross-platform
wails3 build GOOS=windows
wails3 build GOOS=linux
wails3 build GOOS=darwin GOARCH=arm64
# macOS .app bundle
wails3 package GOOS=darwin
# Android APK (needs Android SDK/NDK + JDK 21 — see Mobile below)
wails3 android:package
```

All builds compile with everything included (unified build). If you prefer a
plain binary without the wails CLI, `make desktop` runs `go build ./cmd/desktop`.

> **Version pin rule.** `go.mod` (wails/v3), the `WAILS_VERSION` in the
> `Makefile`, `scripts/desktop-build.sh`, and `.github/workflows/desktop.yml`
> must all carry the same beta — bump them together.

## Cross-platform previews (wails-cross, opt-in)

One Linux/macOS machine builds all three OS targets through the `wails-cross`
Docker image (Zig + macOS SDK, one-time ~800 MB via
`wails3 task setup:docker`):

```bash
make desktop-setup-cross   # one-time Docker image setup
make desktop-cross         # win+mac+linux previews into build/cross/
./scripts/desktop-build.sh cross-windows   # or cross-darwin | cross-linux | cross-universal
```

Use previews for smoke tests without paying for three native runners. **macOS
cross binaries are unsigned** — sign + notarize on a macOS runner before
distributing. Release binaries always come from native runners.

Linux desktop builds run in the dedicated `desktop.yml` workflow on every pull
request and push to `master`, using Ubuntu 24.04 with GTK4 + WebKitGTK 6.0. The
same workflow offers a manual `cross-preview` job (`gh workflow run Desktop`)
that builds win+mac+linux previews and uploads them as an artifact.

## Mobile (Android) is opt-in, not in CI

Wails v3 targets Android from the same `main.go` (Go → `libwails.so`, WebView
frontend) — no separate mobile project. Generate an APK locally with
`wails3 android:package` (or `android:package:fat`). This requires the
**Android SDK (API 35) + NDK (26.3.x) + JDK 21**; `wails3 doctor` reports what's
missing. Because that toolchain is heavy, APK builds are left to the developer
and are not part of the CI matrix. iOS is analogous but requires Xcode.

## Edge sync

If `NATS_LEAFNODE_URL` is set, the desktop boots as a **NATS Leaf Node** that
syncs its JetStream streams with your central server — offline edits replay on
reconnect. Without it, it runs a standalone embedded NATS for local realtime.

On top of that transport, **Loro CRDT** collaboration (`internal/collab`)
publishes whiteboard updates on `app.sync.<docID>` and ephemeral multi-user
**cursors** on `app.presence.<docID>`. The central server persists resolved
Loro snapshots to PocketBase (the `whiteboards` collection) and streams
presence to browser clients via SSE (`GET /api/collab/presence/{docID}`).

## Native window PoC (gogpu/ui)

`cmd/gui` is a **second frontend over the same backend** (PocketBase +
`EntityStore` + `features/auth.Login` / `ResolveOwner`) — no
`*core.RequestEvent`, no cookies, no HTTP router, no webview. It proves the
domain layer is transport-agnostic: login + todo add/toggle/delete through the
exact store the web handlers use, with cross-frontend visibility on a 2s poll
tick.

```bash
make gui      # headless: go test -race ./cmd/gui + CGO_ENABLED=0 build
make run-gui  # open the window (needs DISPLAY/GPU; not exercised in CI)
```

Like `cmd/desktop` it is a separate target: excluded from `web-packages.sh`
(web CI) and validated by the `gui-poc` job in
`.github/workflows/desktop.yml` (gofumpt + vet + govulncheck + race tests +
build, all headless). Full `golangci-lint` is a **manual local step by
design** — linting wgpu/naga is minutes-cold per push and only catches style.
Run `make lint-gui` before committing any `cmd/gui` change.

**Rules for `cmd/gui`:** the UI tree is only touched on the UI thread
(`uiMu` serializes `SetRoot`); background poll produces data under `stateMu`
while views read `snapshot()` copies; lock order is always `uiMu → stateMu`.

**Deliberate limits** (see the `cmd/gui/main.go` header): online-only (no
outbox/replay), no JetStream push (poll only), native `add()` does not trigger
the onboarding workflow, session token in a `0600` file (no OS keyring).

**To remove:** delete `cmd/gui/`, drop the `gui` Makefile target and the
`gui-poc` job. No script change needed — it is already excluded from web CI.

## Related

- [Six async layers](async-layers.md) — where Leaf Node and CRDT fit.
- [Configuration](configuration.md) — `NATS_LEAFNODE_URL`.