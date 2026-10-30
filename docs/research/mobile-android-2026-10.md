# Android mobile: the database is the gate

Reviewed: 2026-10-30

The desktop shell runs on Android only if the data layer runs there. This page
records what the 2026-10-30 emulator spike proved, what it rejected, and the
strategy that follows. **Android is still not a supported target**; the working
spike lives on `spike/mobile` and is not merged.

## What the spike proved

A Wails v3 APK built from a separate the mobile entrypoint entrypoint, installed and
launched on an API 35 arm64 emulator, rendered the real gogogo UI: the landing
page, the mobile dock, the Todo page, and a todo created through the loopback
API. Screenshots:

- `android-spike-2026-10-landing.png` — landing page + dock
- `android-spike-2026-10-todo.png` — Todo page with a row created through the
  loopback API

The app is not a mock: it boots the same chi stack, the same sqlc queries and
the same migrations as the desktop shell.

## The database is the gate

`tursogo` (the template's default driver) embeds native libraries for
`darwin`, `linux` and `windows` only. The evidence is
`turso-go-platform-libs@v0.8.3/load_turso.go`: `libraryFilename()` maps those
three GOOS values; Android falls through to `lib_fallback.go`, which returns no
library. The app panics at boot:

```
panic: unable to load turso library: turso library is not embedded for the platform in the package
```

That is a platform gap in the dependency, not a Wails/NDK/emulator problem.
`internal/db`, `internal/queue` and the credits ledger all opened through
tursogo, so the whole app was blocked.

The spike's driver matrix:

| Package | Desktop / server | Android |
|---|---|---|
| `internal/db` | tursogo (`turso`) | `ncruces/go-sqlite3` (`sqlite3`) |
| `internal/queue` | tursogo | ncruces |
| `features/credits/handlers` | tursogo | ncruces |

The SQL, sqlc queries, migrations and pragmas stay shared; only the driver name
and the DSN spelling differ (`_busy_timeout` vs `_pragma=busy_timeout(...)`).
This is the cheapest path that keeps one schema: a pure-Go SQLite driver.

The alternative is to build the native Turso SDK for Android
(`libturso_sync_sdk_kit.so` for `aarch64-linux-android`) and load it with
`LoadStrategy: system`/`mixed`. It needs Rust + the Android target and a
platform-libs artifact that does not exist today; it is not a template change.

## `cmd/gui` and ncruces: configured, not native

`cmd/gui` does not get ncruces from `database/sql` or from tursogo. The repo
configures it:

- `cmd/gui/main.go` imports `_ "github.com/ncruces/go-sqlite3/driver"`.
- `cmd/gui/store.go` opens `sql.Open("sqlite3", dsn)` with
  `_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)`.
- Its comment gives the reason: `internal/db` opens through tursogo, which links
  purego/fakecgo; that collides with goffi at `CGO_ENABLED=0`. `cmd/gui`
  bypasses `internal/db` to stay CGO-free.

So ncruces is a deliberate `cmd/gui` choice, and the Android spike reuses the
same driver for the same class of reason: a platform where tursogo cannot run.
It is not the template default.

## Mobile entrypoint strategy

1. **A mobile shell is its own entrypoint** (the mobile entrypoint), not
   `cmd/desktop`. The desktop shell owns a window and a single-instance lock;
   Android owns the activity and the WebView origin.
2. **The data layer needs a platform driver matrix.** Keep SQL and migrations
   shared; swap the driver and DSN at the build-tag boundary.
3. **Do not rely on the Wails asset handler for a server-rendered UI.** The
   Android asset path is Java-side (`WailsPathHandler` → `bridge.serveAsset`),
   so a Go `Assets.Handler` proxy is not used. The spike listens on a fixed
   loopback port (`127.0.0.1:18080`) and the activity loads that origin
   directly, with retries and `usesCleartextTraffic`.
4. **Android needs its own main registration.** The overlay generator targets
   the project root; the mobile package's `android`-tagged `init` calls
   `application.RegisterAndroidMain(main)` directly.
5. **The c-shared object needs Android libc shims.** `bcmp`,
   `__errno_location` and `__xpg_strerror_r` are provided by
   the mobile package's `_android.c` shim; `cgo_android.go` enables cgo so the C file is
   linked.
6. **Spike shortcuts are not product decisions.** Open auth (`AUTH_MODE=open`),
   a fixed port, cleartext HTTP and no token storage are acceptable for a
   measurement. A release needs Keystore/Keychain, deep-link activation, TLS
   and a session model that survives the WebView origin.

## What remains

- A product decision on the mobile data layer: ncruces matrix vs building Turso
  for Android.
- Keystore/Keychain token storage and deep-link activation.
- A real origin/session model (cookies or a token) instead of open auth.
- A CI-runnable mobile build (the SDK/NDK/system image is ~8 GB and the
  emulator's System UI ANRs on a loaded host).

## Sources

- `turso-go-platform-libs@v0.8.3/load_turso.go` (GOOS switch, `lib_fallback.go`)
- `cmd/gui/store.go` (ncruces DSN and the fakecgo/goffi reason)
- the mobile entrypoint on `spike/mobile` (entrypoint, driver tags, shims)
- `ANDROID-SPIKE.md` on `spike/mobile` (commands, logs, disk usage, cleanup)
