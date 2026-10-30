# Desktop & Mobile

The same Go backend (flat stack: Turso + owned auth + queue + handlers) runs as a
**native desktop app** via Wails v3. The desktop build reuses 100% of the
business logic — it boots `internal/server.StartChi`, serves the flat stack in a
goroutine, and opens a webview window on it over loopback HTTP.

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
make desktop-smoke      # THE GATE: build, open the window, prove it loaded
make run-desktop        # build, then open the window
# or, with the output under build/ and cross targets available:
./scripts/desktop-build.sh              # native (host OS/arch)
./scripts/desktop-build.sh cross-windows
./scripts/desktop-build.sh cross-darwin
./scripts/desktop-build.sh cross-linux
```

`make desktop` only compiles, and a shell can compile, boot, serve — and never
show a window (`app.Run()` creates none). **`make desktop-smoke`** is the gate
that observes the window: it launches with a temp database, waits for the
webview's own `GET /` and `/static/app.min.css` in the flat stack's access log,
then checks first-run signup and the single-instance lock. Run it before pushing
any `cmd/desktop` change. `SKIP_BUILD=1` reuses the binary; `KEEP=1` keeps the
temp dir.

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

## Mobile (Android): spike validated, not supported

Wails v3 targets Android from a Go shared library plus a WebView frontend.
**`make mobile` is the opt-in packaging path**: it runs the pinned Wails CLI's
`android:build` + `android:assemble:apk` against the `Taskfile.yml` and the
Gradle project under `build/android/`. No CI job compiles it (the SDK/NDK/
emulator is ~10 GB), and `./scripts/desktop-build.sh android` still exits with
that explanation rather than a cryptic error.

The 2026-10-30 emulator spike proved a working path and the constraint that
decides it: **tursogo has no Android library**, so
the data layer must swap to a pure-Go driver (`ncruces/go-sqlite3`) behind
`android` build tags. With a separate the mobile entrypoint entrypoint, a fixed loopback
origin and that driver matrix, the real UI rendered and a todo created through
the API appeared in the list. Master still opens tursogo and ships no mobile
target. Evidence, screenshots and the strategy:
[Mobile Android: the database is the gate](research/mobile-android-2026-10.md).

**Build it locally (opt-in, no CI).** Prerequisites: Android command-line
tools, `platforms;android-35`, `build-tools;35.0.0`, `ndk;27.2.12479018`,
`emulator`, an API 35 arm64 system image, JDK 21, and the pinned Wails CLI
(v3.0.0-beta.24, the version in `go.mod`). Then:

```bash
export ANDROID_HOME=/opt/homebrew/share/android-commandlinetools
export ANDROID_SDK_ROOT=$ANDROID_HOME
export ANDROID_NDK_HOME=$ANDROID_HOME/ndk/27.2.12479018
export JAVA_HOME=/opt/homebrew/opt/openjdk@21

# one-time: install the CLI (the library is pinned in go.mod)
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.24

make mobile                       # wails3 task android:build + assemble:apk
adb install -r bin/gogogo.apk
adb shell am start -n com.wails.app/.MainActivity
```

`cmd/mobile` boots the same chi stack on `127.0.0.1:18080`; `MainActivity`
loads that origin after the bridge boots (retries, cleartext, system-bar
insets). The database driver is selected by build tag: tursogo on
desktop/server, `ncruces/go-sqlite3` on Android (see the research page). The
APK is a spike-quality target: open auth, fixed port, no Keystore, no deep-link
activation, no TLS. It is never built in CI; the SDK/NDK/emulator is ~10 GB.

iOS is analogous but needs Xcode and a Keychain-backed token.

## Edge sync

If `NATS_LEAFNODE_URL` is set, the desktop boots as a **NATS Leaf Node** that
attaches to your central server. Live CRUD ops cross the leaf on **core NATS**
(the hub's `APP_CRUD` stream captures them, its consumer applies them); each
side's JetStream stream stays in its own domain, because NATS does not
replicate streams across a leaf without a mirror/source configuration.
Consequence: **live sync works; offline CRUD replay on reconnect is not wired
yet** — the edge's local stream is local history, and a queued write is not
replayed to the hub automatically. Without a leaf URL the desktop runs a
standalone embedded NATS for local realtime.

On top of that transport, **Loro CRDT** collaboration (`internal/collab`)
publishes whiteboard updates on `app.sync.<docID>` and ephemeral multi-user
**cursors** on `app.presence.<docID>`. The central server persists resolved
Loro snapshots to the shared store and streams presence to browser clients
via SSE (`GET /api/collab/presence/{docID}`).

## Which backend `cmd/desktop` boots

`cmd/desktop` boots the **flat stack** (`internal/server.StartChi`) and opens a
Wails window on it over **loopback HTTP** (`http://127.0.0.1:<port>`), so the
desktop and the web frontend share the same business logic. It sets
`cfg.ChiAddr` to `<host>:0` itself — an **ephemeral port**, because the default
`:8080` is taken by any dev server and the shell must not refuse to open because
of it — and `StartChi` returns the address the listener actually holds, which is
what the window loads. An explicit `PORT` still pins the port.

Loopback HTTP, not a Wails asset handler (`wails://`): WebKit does not persist
cookies set on a custom scheme, so the session cookie never came back and every
page rendered logged out. A real loopback origin keeps cookies, `SameSite=Lax`,
CSP nonces, the service worker and SSE behaving exactly as they do in the
browser.

Four consequences follow, all handled in `cmd/desktop`:

- **The window is created explicitly** (`app.Window.NewWithOptions`).
  `app.Run()` alone starts NSApplication with no window: the process looks
  healthy and serves normally while the screen stays empty.
- **`SecureCookies` is cleared.** WebKit also drops a `Secure` cookie set over
  `http://127.0.0.1` (verified with a `WKWebView` harness), so a production
  config would leave every page logged out. Loopback is the only hop the cookie
  crosses, where `Secure` protects nothing.
- **The listener binds `127.0.0.1` by default.** The shell has exactly one
  client, its own webview; `HOST` still overrides for an operator who wants the
  desktop served on the network.
- **One instance per profile.** The Wails single-instance lock is taken before
  the backend boots, scoped by the database path so two profiles can coexist.
  Without it a duplicate launch reaches the database and dies on Turso's
  `File is locked by another process` error instead of exiting quietly.

`cfg.Host` is the *listen* address; a webview cannot navigate to the wildcard
`0.0.0.0`, so `webviewURL` maps it to `127.0.0.1`.

**First run.** A desktop build carries production defaults (no build tag sets
`ENVIRONMENT`), so `SIGNUP_ENABLED` and the demo accounts are off. With no
operator to create user #1 that is a locked door, so `bootstrapSignup` opens
signup **only while the `users` table is empty**; the next boot closes it again.

**Same-host identity.** The CRUD wire tags every op with `APP_INSTANCE_ID` and
drops an op that carries the receiver's own id (that is how an instance ignores
its own echo). The default id is the hostname, so a server and a desktop on the
same machine would share it and the server would discard the desktop's writes.
`cmd/desktop` therefore scopes the id to its profile (a hash of
`TURSO_DB_PATH`) unless the operator set `APP_INSTANCE_ID` explicitly.

## Accounts and signup

`SIGNUP_ENABLED=true` adds a **Create account** link to `/login` and serves a
standalone `/signup` page (email, optional name, password, confirm). A
successful form submission creates the account, sets the `gogogo_auth` cookie
and redirects to `/`. `SIGNUP_ENABLED=false` makes `/signup` answer 404 and
removes the link; `AUTH_MODE=open` redirects both pages to `/`, because there is
no account to create.

The JSON API (`POST /api/auth/signup/credential`) answers precisely so a form
can point at the fix:

| case | status | `error` |
|---|---|---|
| password shorter than 8 | 422 | `password_too_short` |
| malformed email | 422 | `invalid_email` |
| email already registered | 409 | `email_taken` |
| success | 200 | — (sets `gogogo_auth`) |

Every failure body is `{"error":"code","message":"human sentence"}`. This is a
deliberate product decision: **signup may reveal that an address exists**,
because a form that cannot say "sign in instead" is a dead end. **Login stays
indistinguishable** — unknown address, wrong password and malformed input all
answer the same 401, so signup is not an enumeration oracle for the login path.
`REQUIRE_EMAIL_VERIFICATION=true` sends the verification link and does not
leave the new account signed in.

**A broken leaf node degrades to embedded NATS**, not to nothing: losing
cross-instance sync must not cost local realtime.

Everything else in the file — the Leaf Node / embedded NATS choice, the
collab demo publish, the presence ticker, the window, `OnShutdown` — is
backend-agnostic and unchanged. The previous command-line passthrough
(the old `RootCmd.SetArgs(os.Args[1:])` for `serve --http …`) is gone with it:
there is no subcommand to forward, the stack serves on the configured
address.

## Activation (distribution gate)

The owner decides per release with `LICENSE_MODE`:

| mode | who can use it | what the user does |
|---|---|---|
| `none` (default) | anyone with the build | installs and uses it; local accounts only |
| `license` | buyers | pastes an owner-minted token at `/activate`, once |
| `account` | invited users | pastes an account-bound token; activation adopts the server id and signs in |

Tokens are Ed25519-signed (`gogogo1.<payload>.<sig>`) and verified locally, with
no phone-home. `go run ./cmd/license keygen` prints the public key that ships in
`LICENSE_PUBLIC_KEY`; the private key stays with the owner, who mints with
`mint -email … -plan … [-days N] [-sync URL]`. Expiry is optional (no `exp` =
perpetual). A stored token that fails verification boots **gated**, never
half-open.

The gate runs **on the shell**, not on the issuer: a web deploy in `account`
mode needs to stay reachable for login and device-token issuance, so it
verifies nothing locally and serves normally. `LICENSE_GATE` is set by
`cmd/desktop`; the server only needs the private key.

`AUTH_MODE=open` is the other owner choice: no login at all, one fixed local
owner, every request already signed in. It is independent of the gate (`open` +
`license` is a valid pair) and it refuses a non-loopback `HOST` unless
`AUTH_OPEN_ALLOW_REMOTE=true`, because a no-login app on the network is an
unauthenticated instance for anyone who can reach the port.

## Cloud sync (connect this device)

The connection is configured at **`/account`**, not `/config`. `/config` is the
operator surface: it needs an admin session **and** an `ADMIN_EMAILS` match, so
on a fresh desktop it is closed — and the person connecting a device is the
user, not the operator. `/account` already owns the user's security settings;
"Cloud sync" sits next to them.

The form takes a server URL (`nats://`, `tls://`, `ws://`, `wss://`) and an
optional device token, writes `<DATA_DIR>/sync.json` (0600) and applies on the
**next launch**, because the NATS transport is built at boot. A token minted
with `cmd/license mint -sync nats://…` writes the same file during activation,
so an invited user never types the endpoint. `CLOUD_SYNC_ALLOW` can pin the
hosts the form accepts.

**Getting a token without the owner.** In `account` mode the signed-in user
opens `/account` → *Device token*: the server signs a token bound to their id
(with `CLOUD_SYNC_URL` inside) and shows it once. That is the server-side
issuance path; the owner's CLI remains for `license` mode and offline sales.

### Sync prerequisites (one place)

A device can only sync when **all** of these are true:

| requirement | where it is set | why |
|---|---|---|
| the hub accepts leaf nodes | `NATS_LEAFNODE_LISTEN=host:port` | NATS serves the leaf protocol on its own listener; the client port (`NATS_LISTEN`) cannot accept it. |
| clients and leafs authenticate | `NATS_TOKEN=…` | the guard refuses a non-loopback client **or** leaf listener without a token. The same token is the leaf credential (username in `nats://token@host`). |
| the device knows the endpoint | `CLOUD_SYNC_URL=nats://…` on the server, or the `/account` form | it rides in the device token, so the user does not type it. |
| the device can reach it | network path | the lean path is a private overlay (Tailscale/WireGuard): it already encrypts the wire, so `NATS_LEAFNODE_LISTEN=100.x.y.z:7422` + `NATS_TOKEN` is enough. |

`make desktop-sync-smoke` is the end-to-end proof of this table: it starts a
server in `LICENSE_MODE=account`, issues a device token, activates a desktop,
restarts it as a leaf, creates a todo on the desktop and asserts the row is on
the server under the same user id.

Public exposure is a different bar: TLS per connection type, per-device
credentials (NKey/JWT), and subject permissions so a device sees only its own
tenant. The NATS docs are explicit about each of those; do not open a plaintext
port. **Never point a build at `gogogo.calionauta.com`** — that host is only the
public demo, and a real deployment owns its own endpoint.

**Mobile (Wails Android/iOS).** The transport and activation code is portable —
HTTP + signed token — but the shell is not: loopback/cookie behavior in the
Android/iOS WebView must be verified, the token belongs in the Keystore/Keychain
(not a 0600 file), and activation should arrive by deep link/QR instead of a
paste. `make mobile` now builds an opt-in APK locally (no CI), and the
2026-10-30 spike validated the ncruces driver path and the `cmd/mobile`
entrypoint on an emulator; it is still not a supported target until a device
smoke and the Keystore/deep-link work exist. See
[Mobile Android: the database is the gate](research/mobile-android-2026-10.md).

**Self-contained by design.** No template instance talks to
`gogogo.calionauta.com`; that host is only the public demo. `CLOUD_SYNC_URL`,
`APP_URL` and every endpoint are operator config, empty by default.

**Revocation.** A token is verified offline, so revocation cannot be instant.
Bound it with `DEVICE_TOKEN_DAYS` and re-issue; a deleted `sync.json`/
`license.token` re-gates the device. Short-lived tokens plus re-issue is the
2026-accepted trade-off for offline activation.

**Switching modes.** Activation is meant to happen before use: the account gate
blocks the app, so there is no pre-existing data to migrate. Running `none`
first and switching to `account` later adopts a new user id and leaves the old
rows owned by the old local user — export/import them deliberately. An explicit
`NATS_LEAFNODE_URL` always wins over the file, so an operator's environment is
never overridden by a UI setting.

Identity is the catch: the CRUD wire skips any op whose owner id is unknown
locally, so rows converge only when the account id matches on both sides.
`LICENSE_MODE=account` activation creates the local row with the token's `sub`,
which is the supported way to reach that state; a purely local account never
joins the server's rows.

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