# Android spike (2026-10-30)

**Verdict: the ncruces path works on the emulator; Android is still not a
supported target and this branch is not merged.** The APK builds, installs and
renders the real gogogo UI (landing page, Todo list, create via API) against a
pure-Go SQLite database. Master still boots `internal/db` through tursogo, which
has no Android library, so nothing on master claims Android support.

Screenshots:

- `docs/research/android-spike-2026-10-landing.png` — landing page + mobile dock
- `docs/research/android-spike-2026-10-todo.png` — Todo page with a row created
  through the loopback API (proves the database, not just the shell)

## Environment installed

| What | Version / path |
|---|---|
| Android command-line tools | `brew install --cask android-commandlinetools` → `/opt/homebrew/share/android-commandlinetools` |
| JDK | `brew install openjdk@21` → `/opt/homebrew/opt/openjdk@21` (21.0.13) |
| SDK packages | `platform-tools`, `platforms;android-35`, `build-tools;35.0.0`, `ndk;27.2.12479018`, `emulator`, `system-images;android-35;google_apis;arm64-v8a` |
| AVD | `gogogo-api35` (Pixel 7, arm64-v8a) |
| Wails CLI | v3.0.0-beta.24; `wails3 doctor` reports "ready" with the SDK/JDK set |

## Commands that build and run the working spike

```bash
export ANDROID_HOME=/opt/homebrew/share/android-commandlinetools
export ANDROID_SDK_ROOT=$ANDROID_HOME
export ANDROID_NDK_HOME=$ANDROID_HOME/ndk/27.2.12479018
export JAVA_HOME=/opt/homebrew/opt/openjdk@21

wails3 task android:build        # compiles cmd/mobile → libwails.so (arm64)
wails3 task android:assemble:apk # Gradle assembleDebug → bin/gogogo.apk
adb install -r bin/gogogo.apk
adb shell am start -n com.wails.app/.MainActivity
```

`Taskfile.yml` and `build/android/` are the minimum Wails mobile project
(Taskfile + Gradle app). The Go build target is `./cmd/mobile`, with
`-tags android,debug`.

## What had to change (and why)

The desktop entrypoint cannot be reused as-is. Four layers failed in order;
each fix exposed the next.

**1. libc symbols.** The c-shared object references symbols Android's libc does
not export:

```
java.lang.UnsatisfiedLinkError: dlopen failed: cannot locate symbol "bcmp"
```

`cmd/mobile/bcmp_android.c` provides `bcmp`, `__errno_location` and
`__xpg_strerror_r`; `cmd/mobile/cgo_android.go` enables cgo so the C file is
linked. This is an Android packaging shim, not a product change.

**2. Android main registration.** Wails logged:

```
E Wails: No main function registered! Call application.RegisterAndroidMain(main) in init()
```

The overlay generator targets the project root, but the main package is
`cmd/mobile`. `cmd/mobile/register.go` (build tag `android`) calls
`application.RegisterAndroidMain(main)` directly.

**3. Turso has no Android library.** With `main` registered:

```
E Go: panic: unable to load turso library: turso library is not embedded for the platform in the package
```

`turso-go-platform-libs@v0.8.3/load_turso.go` maps only `darwin`, `linux` and
`windows`; Android falls through to `lib_fallback.go`. `internal/db`,
`internal/queue` and `features/credits/handlers` all opened through tursogo.

The spike swaps those three opens to `ncruces/go-sqlite3` (pure Go/WASM) behind
`android` build tags:

| Package | Desktop/server | Android |
|---|---|---|
| `internal/db` | tursogo (`turso`) | ncruces (`sqlite3`) |
| `internal/queue` | tursogo | ncruces |
| `features/credits/handlers` | tursogo | ncruces |

The SQL, sqlc queries, migrations and pragmas are shared; only the driver and
DSN spelling differ (`_busy_timeout` vs `_pragma=busy_timeout(...)`).

**4. The status bar eats the top header.** With the UI rendering, the bottom
dock and the todo checkbox worked but taps on the top header (hamburger,
search) did nothing: the activity draws edge-to-edge and the system status bar
consumes touches over the header. `android:fitsSystemWindows="true"` on the
root layout insets the WebView below the system bars; the hamburger and search
then respond. A real shell should apply explicit `WindowInsets` padding rather
than rely on the flag, but the failure mode is worth knowing: **a rendered page
is not proof that the top chrome is reachable.**

**5. The Wails asset origin is Java-side.** The Android activity loads
`https://wails.localhost/`; its `WailsPathHandler` calls `bridge.serveAsset`,
so a Go `Assets.Handler` proxy is not used. `cmd/mobile` listens on a fixed
loopback port (`127.0.0.1:18080`) and `MainActivity` loads
`http://127.0.0.1:18080/` after the bridge boots, with retries and
`android:usesCleartextTraffic="true"`. This is a spike shortcut: a real mobile
shell should own the origin and the auth/session model.

`cmd/mobile` also sets `AUTH_MODE=open` (fixed local owner), disables NATS,
DagNats, GoAkt and offline sync, and uses `application.Mobile.StoragePath()` for
the database directory.

## `cmd/gui` and ncruces: configured, not native

`cmd/gui` uses ncruces because the repo chose it, not because SQLite or
`database/sql` defaults to it:

- `cmd/gui/main.go` imports `_ "github.com/ncruces/go-sqlite3/driver"`.
- `cmd/gui/store.go` opens `sql.Open("sqlite3", dsn)` with
  `_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)`.
- The comment there states the reason: `internal/db` opens through tursogo,
  which links purego/fakecgo; that collides with goffi at `CGO_ENABLED=0`.
  `cmd/gui` therefore bypasses `internal/db` and keeps a CGO-free build.

So: **ncruces is a deliberate `cmd/gui` configuration**, and the Android spike
reuses the same driver for the same class of reason (a platform where tursogo
cannot run). It is not the template's default driver.

## Mobile strategy learned

1. **A mobile shell is its own entrypoint** (`cmd/mobile`), not `cmd/desktop`.
   The desktop shell owns a window and a single-instance lock; Android owns the
   activity and the WebView origin.
2. **The data layer needs a platform driver matrix.** tursogo is desktop/server
   only today. A pure-Go driver (ncruces) keeps SQL/migrations shared; the
   alternative is building the Turso native SDK for Android and loading it with
   `LoadStrategy: system`.
3. **Do not rely on the Wails asset handler for a server-rendered UI.** The
   Android asset path is Java-side. A real shell either loads the loopback
   origin (spike) or implements a deliberate origin/proxy with cookies.
4. **Spike shortcuts are not product decisions.** Open auth, a fixed port,
   cleartext HTTP and a paste-free token are acceptable for a measurement; a
   real mobile release needs Keystore/Keychain, deep-link activation, TLS and a
   session model that survives the WebView origin.
5. **Emulator cost is real.** The SDK/NDK/system image is ~8 GB and the AVD
   ~2 GB; the emulator's System UI can ANR on a loaded host (dismiss "Wait").

## Disk usage and cleanup

Measured after the spike:

| Path | Size |
|---|---|
| `/opt/homebrew/share/android-commandlinetools` (SDK + NDK + emulator + system image) | 8.1 GB |
| `~/.android/avd/gogogo-api35.avd` | 2.0 GB |
| `bin/gogogo.apk` (generated, not committed) | 49 MB |

Cleanup:

```bash
# stop the emulator first
adb emu kill || true

brew uninstall --cask android-commandlinetools
brew uninstall openjdk@21
rm -rf /opt/homebrew/share/android-commandlinetools
rm -rf ~/Library/Android                 # only if a non-Homebrew SDK was used
rm -rf ~/.android/avd/gogogo-api35.avd ~/.android/avd/gogogo-api35.ini
rm -rf ~/.gradle/caches ~/.gradle/wrapper   # optional, shared by other Gradle work
rm -rf build/android/app/build build/android/.gradle build/android/gen \
       build/android/overlay.json bin/gogogo.apk
```

Do not merge this branch; it is a measurement, not a supported target.
