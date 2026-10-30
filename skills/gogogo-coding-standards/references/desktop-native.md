# Native targets: desktop (Wails) and window (gogpu/ui)

Two binaries sit outside the web build. Neither is compiled by any CI job, so a
change that breaks one fails **nowhere** until someone builds it. That makes the
build a discipline, not a formality: run the target before pushing.

- `cmd/desktop` — Wails v3 shell. Renders the real web UI in a webview over
  loopback HTTP against `internal/server.StartChi`. Gets every feature the web
  app has. Needs CGO and OS webview headers.
- `cmd/gui` — gogpu/ui window. Draws native widgets, no HTTP, no webview. Proves
  the domain layer is transport-agnostic. Pure Go, `CGO_ENABLED=0`, ships in
  releases as `gogogo-gui`.

## Why there is no CI job

The Wails build needs GTK4 + WebKitGTK on Linux and the macOS SDK on Mac, and the
gogpu tree pulls wgpu/naga — minutes of install and compile for a binary no
release attaches. The cost bought a green check nobody consumed.

The trade is explicit: **the recipe below is the gate.** Docs and this skill carry
it instead of a workflow, so treat a missed build as a missed gate.

## Prerequisites, by host

| Host | Needs |
|---|---|
| macOS | Xcode Command Line Tools (`xcode-select --install`); CGO on. Nothing else. |
| Linux (Debian/Ubuntu) | `build-essential pkg-config libgtk-4-dev libwebkitgtk-6.0-dev` |
| Windows | WebView2 runtime (ships with Windows 11) plus a C toolchain for CGO |

`cmd/gui` needs none of that — `CGO_ENABLED=0` and no system headers.

## Build and run

```bash
make desktop        # → build/desktop/gogogo-desktop  (CGO + webview headers)
make desktop-smoke  # THE GATE: build + prove the WINDOW loaded the app
make run-desktop    # build, then open the window
make gui            # race tests, then → build/gui/gogogo-gui (CGO off)
make run-gui        # open the native window (needs a display + GPU)
make lint-gui       # manual full-lint pass on cmd/gui; never in CI
```

Output goes to `build/`, never the repo root: a stray 115 MB binary is how a
commit grows a payload nobody asked for.

## Rules

**`make desktop` is compile-only, not proof.** A shell can build, boot and serve
with no window at all. `make desktop-smoke` launches it with a temp database and
asserts the webview's own `GET /` and `/static/app.min.css` reached the flat
stack's access log (plus first-run signup and the single-instance lock). Run it
before pushing any `cmd/desktop` change; `SKIP_BUILD=1` reuses the binary and
`KEEP=1` keeps the temp dir.

**Never run `go build ./...` or `go test ./...`** as a shortcut — both compile
`cmd/desktop` and fail without the platform headers. `make build` / `make test`
use `scripts/web-packages.sh`, which excludes the two targets.

**Touching `cmd/desktop` means running `make desktop-smoke`.** There is no CI
job to catch a broken shell. Same for `cmd/gui` and `make gui`.

**The Wails pin lives in `go.mod`.** No Makefile variable, no workflow copy to
keep in sync.

**Native code inside `cmd/web` is still banned.** These are separate binaries;
see `exception-to-go.md` for what "native" is allowed to mean.

## Shell invariants (each one has already bitten this repo)

1. **Create the window explicitly** — `app.Window.NewWithOptions(...)`.
   `app.Run()` starts NSApplication with no window and serves forever.
2. **Load a real HTTP origin** — loopback, never a Wails asset handler
   (`wails://`). WebKit persists no cookie from a custom scheme.
3. **Clear `Secure` cookies** when the origin is loopback HTTP: WebKit drops a
   `Secure` cookie on `http://127.0.0.1` (verified with a `WKWebView` harness).
4. **Bind `127.0.0.1` and an ephemeral port** (`:0`, then read the bound address
   back). A fixed `:8080` is taken by any dev server; the window must load the
   address the listener actually holds, not the one it asked for.
5. **Take the single-instance lock before booting the backend**, scoped by the
   database path. Otherwise a duplicate launch opens a second listener and a
   second writer until Turso refuses the file.
6. **Quit on last window close** (`ApplicationShouldTerminateAfterLastWindowClosed`),
   or closing the window leaves a headless process.
7. **First run needs a door.** A release build carries production defaults
   (signup off), so open signup while the `users` table is empty.
8. **A failed leaf node degrades to embedded NATS**, not to nothing: losing
   cross-instance sync must not cost local realtime.
9. **The distribution gate sits above the router** (`internal/server/license_gate.go`):
   pages redirect to `/activate`, APIs get JSON 403, and only `/activate`,
   `/static/*`, `/health` and `/sw.js` pass. A stored token that fails
   verification boots **gated**, never half-open.
10. **`AUTH_MODE=open` is opt-in to the network**: one fixed local owner with no
    session. Refuse a non-loopback bind without `AUTH_OPEN_ALLOW_REMOTE=true`,
    or the app is an unauthenticated instance for anyone who reaches the port.
11. **Cloud credentials live in `<DATA_DIR>/sync.json` (0600)**, never in the
    database, and an explicit `NATS_LEAFNODE_URL` wins over the file. Config UI
    belongs on `/account` (the user surface), not `/config` (operator-only).
12. **Account-mode tokens are issued by the server** (`/account` → Device
    token, `LICENSE_PRIVATE_KEY` + `CLOUD_SYNC_URL`), not by the buyer; the
    desktop only verifies. Activation can set the local password, and
    `CLOUD_SYNC_ALLOW` pins which sync hosts a session may save.
13. **Open mode has no login UI**: `/login` redirects to `/` and the account
    security cards are hidden. It is a distribution choice, not a bug.

## When a product needs sync

Add sync only when the product needs **multi-device continuity or cloud
backup**; local-first is the default and needs no server. If it does:

1. **Expose a leaf listener, not just a client port.** Set
   `NATS_LEAFNODE_LISTEN` on the hub, `NATS_TOKEN` on both sides, and
   `CLOUD_SYNC_URL` to the leaf URL. A leaf edge publishes live CRUD on core
   NATS; each side's JetStream stream stays in its own domain.
2. **Align identity before writing.** An account token must adopt the server's
   `user.id` (`LICENSE_MODE=account` + activation). A purely local account
   never joins the server's rows: `tenantOf` skips unknown owners.
3. **Scope the instance id per profile.** Two processes on one host share the
   hostname-derived `APP_INSTANCE_ID`; the receiver then drops the sender's
   ops as its own echo. `cmd/desktop` hashes the database path; do the same for
   any new edge.
4. **Use a private overlay first.** Tailscale/WireGuard plus a token is the
   lean path. Public exposure needs TLS per connection type and per-device
   credentials — never a plaintext port, never the demo host.
5. **Prove it with `make desktop-sync-smoke`.** It creates a row on the
   desktop and asserts the server has it under the same user id. A green unit
   test does not prove a leaf attached.

The guard: sync is a product decision, not a template default. If the answer
is "one device, one user", keep the embedded NATS and skip the server.

## Common failures

- **App runs, backend logs requests from tests, but no window ever appears** —
  `app.Run()` creates nothing by itself; the shell must call
  `app.Window.NewWithOptions(...)`. Without it NSApplication keeps its run loop
  alive and the flat stack serves normally, so the process looks healthy while
  the screen stays empty. Pair it with
  `Mac.ApplicationShouldTerminateAfterLastWindowClosed`, or closing the window
  leaves the app running headless.
- **Window appears but every page is logged out** — the webview is on a custom
  scheme (`wails://`) served by a Wails `Assets.Handler`; WebKit does not
  persist cookies set on a custom scheme. Load the flat stack over loopback
  HTTP instead (`http://127.0.0.1:<port>`), which is also what keeps CSP
  nonces, the service worker and SSE behaving as in the browser.
- **`ld: warning: object file built for newer 'macOS'...`** — harmless
  deployment-target noise from Wails' prebuilt objects. The binary runs.
- **`fatal error: gtk/gtk.h` not found** — the Linux prerequisites are missing;
  the build stopped at CGO, not at your change.
- **Window opens but the page is blank** — the flat stack did not come up. Check
  `PORT` and that nothing else holds it (the default is `:8080`).
- **`goffi` link error at `CGO_ENABLED=0`** — `cmd/gui` must not import
  `internal/db` (tursogo/purego). Use the `ncruces` driver, as `cmd/gui` does.
