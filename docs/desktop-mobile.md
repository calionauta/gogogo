# Desktop & Mobile

The same Go backend (flat stack: Turso + owned auth + queue + handlers) runs as a
**native desktop app** via Wails v3. The desktop build reuses 100% of the
business logic — it boots `internal/server.StartChi`, serves the flat stack in a
goroutine, and points the webview at it through a reverse proxy.

It is a **separate build target**, not part of the default web build/test loop,
and **no CI job compiles it**. It pulls in Wails v3, which requires GTK + WebKit
dev libs (`libgtk-4-dev libwebkitgtk-6.0-dev`) that only exist on desktop build
hosts. `cmd/desktop` carries **no build tag**, so `go build ./...` and
`go test ./...` from the repo root *do* compile it and will fail without those
libs. What excludes it is the package list: `scripts/web-packages.sh` filters out
`/cmd/desktop` and `/cmd/gui`, and that list is what `make build`, `make test`,
`make lint` and web CI use. So the exclusion is explicit in the tooling, not
implicit in the Go toolchain — run `make test`, not a bare `go test ./...`, on a
box without the desktop headers.

The build recipe is `make desktop`, documented below. It is the only gate: a
change that breaks the shell compiles nowhere else, so run it before pushing if
you touched `cmd/desktop` or one of its direct imports.

## Build commands

Building them means installing GTK/WebKit (Linux) or having the macOS SDK, and a
gate on that install is not worth the minutes when the binary ships nowhere. The
build is local, and the commands below are the whole recipe.

**Prerequisites, by host:**

| Host | Needs |
|---|---|
| macOS | Xcode Command Line Tools (`xcode-select --install`) — the SDK, CGO on. Nothing else. |
| Linux (Debian/Ubuntu) | `build-essential pkg-config libgtk-4-dev libwebkitgtk-6.0-dev` |
| Windows | WebView2 runtime (ships with Windows 11) plus a C toolchain for CGO |

```bash
# Native desktop binary — plain go build, no wails CLI needed
make desktop            # → build/desktop/gogogo-desktop
make run-desktop        # build, then open the window
# or, with the output under build/ and cross targets available:
./scripts/desktop-build.sh              # native (host OS/arch)
./scripts/desktop-build.sh cross-windows
./scripts/desktop-build.sh cross-darwin
./scripts/desktop-build.sh cross-linux
```

The Wails v3 shell is an ordinary Go program importing `pkg/application`, so
the CLI is not required to compile it. `make desktop` runs
`go build -o build/desktop/gogogo-desktop ./cmd/desktop`.

> **`wails3 build` does not work in this repo.** It takes no `-o` flag, and it
delegates to `wails3 task build`, which needs a `Taskfile.yml` at the repo root
— this repo uses `make` instead. `make wails-build` is kept only as an explicit
"why not" target that exits with the reason.

> **Version pin.** The pin lives in `go.mod`
> (`github.com/wailsapp/wails/v3`). Nothing else carries a copy, because nothing
> else builds it — no CI job, no Makefile variable. Bump it by editing `go.mod`.

## Cross-platform previews (wails-cross, opt-in)

One Linux/macOS machine builds all three OS targets through the `wails-cross`
Docker image (Zig + macOS SDK, one-time ~800 MB). The image is built from the
wails source checkout — `wails3 task setup:docker` cannot be used here because
it needs a `Taskfile.yml` at the repo root:

```bash
make desktop-setup-cross   # one-time: clones wails + builds the image
make desktop-cross         # win+mac+linux previews into build/cross/
./scripts/desktop-build.sh cross-windows   # or cross-darwin | cross-linux
```

`cross-universal` (macOS amd64+arm64 via lipo) is **not** wired: it needs
`wails3 task darwin:build:universal`. Build both arches individually and combine
with `wails3 tool lipo` instead — the script's error message says exactly this.

Use previews for smoke tests without paying for three native machines. **macOS
cross binaries are unsigned** — sign + notarize on a Mac before distributing.
Nothing in this repository signs or notarizes, and nothing ships these previews:
they are a local convenience.

**Nothing runs any of this in CI.** The cross image is an ~800 MB download plus a
Docker build, and the native build needs GTK/WebKit or the macOS SDK — both are
costs a shared runner should not pay for a binary no release attaches. Run the
commands above on your own host; if you distribute the shell, add signing as your
own step.

## The other native-shell option: nanoui (Zig)

Wails is the full-app desktop path: same Go binary, webview front end. When
the requirement is a *native* UI instead (drawn by the OS/GPU, not HTML in a
webview), the named direction is [`justrach/nanoui`](https://github.com/justrach/nanoui)
— desktop apps in Zig, immediate-mode views, Metal on macOS and a CPU
rasterizer on Linux/Windows, optional webview pages with a generated typed TS
client, headless `ui.Tester`, ~0.7 MB, MIT.

```zig
const Counter = struct {
    n: i64 = 0,
    fn view(s: *Counter, c: *ui.Context) void {
        const col = ui.div(c).column().sizeFull().center().gap(16);
        defer col.end();
        ui.textf(c, .{ .size = 56, .weight = .bold }, "{d}", .{s.n});
        if (ui.button(c, "Increment", .{ .variant = .primary }).clicked) s.n += 1;
    }
};
```

Relation to the Wails path, so the two are not confused:

- **Both are separate binaries.** nanoui is a peer of `cmd/desktop`, not a
  library linked into `cmd/web` — nothing crosses a C ABI, and the Go
  binary stays the product.
- **Wails reuses the whole Go app; nanoui would not.** A nanoui shell talks
  to the Go server over HTTP (it can serve a page from it), so it does not
  pull `internal/server` in-process the way `cmd/desktop` does.
- **Pinning.** nanoui is 0.x; pin a release tag in `build.zig.zon`, the same
  discipline as the `wails/v3` pin in `go.mod`.
- **Not wired here.** No Zig toolchain in this repo (see
  [exception-to-go](exception-to-go.md)); adopting it is a project with its
  own justification, starting from `zig build run-gallery` on a clone.

## Mobile (Android) is opt-in, not in CI

Wails v3 targets Android from the same `main.go` (Go → `libwails.so`, WebView
frontend) — no separate mobile project. **Packaging an APK is not wired in this
repo**: the pinned CLI (v3.0.0-beta.24) has no `android:package` subcommand
(only `android:overlay:gen`), and the packaging path needs a `Taskfile.yml`.
`./scripts/desktop-build.sh android` exits with that explanation rather than a
cryptic error. If you want APKs, scaffold a Taskfile (`wails3 init`) or build
with the Wails CLI on a project that has one. iOS is analogous but needs Xcode.

## Edge sync

If `NATS_LEAFNODE_URL` is set, the desktop boots as a **NATS Leaf Node** that
syncs its JetStream streams with your central server — offline edits replay on
reconnect. Without it, it runs a standalone embedded NATS for local realtime.

On top of that transport, **Loro CRDT** collaboration (`internal/collab`)
publishes whiteboard updates on `app.sync.<docID>` and ephemeral multi-user
**cursors** on `app.presence.<docID>`. The central server persists resolved
Loro snapshots to the shared store and streams presence to browser clients
via SSE (`GET /api/collab/presence/{docID}`).

## Which backend `cmd/desktop` boots

`cmd/desktop` boots the **flat stack** (`internal/server.StartChi`) and
points the Wails webview at it through a reverse proxy, so the desktop and
the web frontend share the same business logic. It sets `cfg.ChiAddr` to
`<host>:<port>` itself rather than relying on `CHI_ADDR` from the
environment, which keeps the seam explicit at the one place it needs to be.

Everything else in the file — the Leaf Node / embedded NATS choice, the
collab demo publish, the presence ticker, the proxy, `OnShutdown` — is
backend-agnostic and unchanged. The previous command-line passthrough
(the old `RootCmd.SetArgs(os.Args[1:])` for `serve --http …`) is gone with it:
there is no subcommand to forward, the stack serves on the configured
address.

## Native window (gogpu/ui)

`cmd/gui` is a **second frontend over the same backend** (Turso +
sqlc queries + `internal/auth` (`Login` / `SessionByToken`) — no
`*core.RequestEvent`, no cookies, no HTTP router, no webview. It proves the
domain layer is transport-agnostic: login + todo add/toggle/delete through the
exact store the web handlers use, with cross-frontend visibility on a 2s poll
tick.

```bash
make gui      # headless: go test -race ./cmd/gui + CGO_ENABLED=0 build
make run-gui  # build, then open the window (needs a display + GPU)
```

Like `cmd/desktop` it is a separate target: excluded from `web-packages.sh`
(web CI) and **built by no CI job**. `make gui` is the gate — it runs the race
tests and the build, both headless. Full `golangci-lint` is a **manual local step
by design** — linting wgpu/naga is minutes-cold per push and only catches style.
Run `make lint-gui` before committing any `cmd/gui` change. Unlike the Wails
shell below, gui is pure Go (`CGO_ENABLED=0`), so it ships in the release
archives next to the CLI (`gogogo-gui`, all six OS×arch, installed by
`install.sh`).

**Realtime:** unlike `cmd/desktop` (which serves the real web UI in a webview,
so it already uses SSE end to end), `cmd/gui` renders natively and has no HTTP
tier of its own. When a server is reachable it holds `GET /api/todos/events`
open — JSON record frames from the same event bus, scoped to the session — and
re-lists on each frame; the 2s local poll stays as the standalone fallback
(no server, or the stream is down, with exponential backoff up to 30s). It
must not parse Datastar DOM patches. Its own writes still go straight to the
shared store, so they are locally visible on the next refresh but are not
published to the web tabs; an embedded GUI (`Bus.Subscribe`) or thin-client
writes are the follow-ups.

**Rules for `cmd/gui`:** the UI tree is only touched on the UI thread
(`uiMu` serializes `SetRoot`); background poll produces data under `stateMu`
while views read `snapshot()` copies; lock order is always `uiMu → stateMu`.

**Deliberate limits** (see the `cmd/gui/main.go` header): online-only (no
outbox/replay), no JetStream push (poll only), native `add()` does not trigger
the onboarding workflow, session token in a `0600` file (no OS keyring).

**To remove:** delete `cmd/gui/`, drop the `gui`, `gui-build`, `gui-test`,
`run-gui` and `lint-gui` Makefile targets. No script change needed — it is
already excluded from web CI.

## Dropping the Wails shell (opt-out)

The Wails desktop is opt-in weight: beta framework, CGO + system headers,
signing/notarization swamp if you ever distribute. If your product is
web-deployed, drop it — `gui` remains the native story and keeps shipping:

```bash
rm -rf cmd/desktop/          # the shell (nothing outside imports wails)
# then drop the desktop* and run-desktop Makefile targets
go mod tidy                   # drops wails from go.mod
go build ./...                # must stay green (proven clean)
```

No script change is required for the drop itself; `scripts/desktop-build.sh`
serves only these targets, so delete it alongside when nothing references
them anymore.

## Related

- [Seven async layers](async-layers.md) — where Leaf Node and CRDT fit.
- [Configuration](configuration.md) — `NATS_LEAFNODE_URL`.