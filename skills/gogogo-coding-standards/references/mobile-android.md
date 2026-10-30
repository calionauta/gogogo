# Mobile (Android): build and run the opt-in target

Android is a **local, opt-in target** like `cmd/desktop` and `cmd/gui`: no CI
job compiles it, and the SDK/NDK/emulator is ~10 GB. This reference is the
recipe and the invariants; the evidence is
[`docs/research/mobile-android-2026-10.md`](../../../docs/research/mobile-android-2026-10.md)
and the build log is
[`docs/research/android-spike-2026-10.md`](../../../docs/research/android-spike-2026-10.md).

## Prerequisites

| Host | Needs |
|---|---|
| macOS | Xcode CLT, JDK 21 (`brew install openjdk@21`), Android command-line tools (`brew install --cask android-commandlinetools`) |
| Linux | JDK 21, Android command-line tools, a C toolchain for CGO |
| SDK packages | `platform-tools`, `platforms;android-35`, `build-tools;35.0.0`, `ndk;27.2.12479018`, `emulator`, `system-images;android-35;google_apis;arm64-v8a` |
| Emulator | an AVD (`avdmanager create avd -n gogogo-api35 -k "system-images;android-35;google_apis;arm64-v8a"`), boot with `emulator -avd gogogo-api35` |
| Wails CLI | v3.0.0-beta.24 (the `go.mod` pin); `wails3 doctor` must be green |

## Layout

| Path | What |
|---|---|
| `Taskfile.yml` | Wails task entry; only the `android` include |
| `build/config.yml` | Wails product metadata used by `android overlay:gen` |
| `build/android/` | Gradle app + `MainActivity` + `WailsPathHandler`; `Taskfile.yml` builds `cmd/mobile` |
| `cmd/mobile/` | Android-only `main`; boots `internal/server.StartChi` and registers `application.RegisterAndroidMain` |
| `internal/db/`, `internal/queue/`, `features/credits/handlers/` | `_android.go` driver files (ncruces) + `_other.go` (tursogo) |

## Build and run

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
adb logcat -d | grep -E "Go|Wails|AndroidRuntime|panic"
```

The APK is `bin/gogogo.apk` (generated, ignored); the launcher label is
`gogogo`. `wails3 task android:build` compiles `cmd/mobile` as
`libwails.so` for the host ABI; `assemble:apk` runs Gradle.

## Invariants (each one has already bitten the spike)

1. **`cmd/mobile` is its own entrypoint.** Do not reuse `cmd/desktop`: it owns
   a desktop window and a single-instance lock. Register `main` with
   `application.RegisterAndroidMain(main)` in an `android`-tagged `init`.
2. **The database is the gate.** tursogo embeds libraries only for
   darwin/linux/windows (`turso-go-platform-libs/load_turso.go`). Keep SQL,
   sqlc queries and migrations shared; swap the driver and DSN at the build-tag
   boundary. Android uses `ncruces/go-sqlite3` (`sqlite3`); desktop/server uses
   tursogo (`turso`). Do the same for `internal/queue` and the credits ledger.
3. **`cmd/gui` already uses ncruces — by configuration.** It imports
   `github.com/ncruces/go-sqlite3/driver` and opens `sql.Open("sqlite3", ...)`
   explicitly; its store comment names the purego/fakecgo vs goffi collision.
   Reuse that driver for Android for the same class of reason.
4. **Do not rely on a Go `Assets.Handler` for the Android WebView.** The asset
   path is Java-side (`WailsPathHandler` → `bridge.serveAsset`). `cmd/mobile`
   listens on a fixed loopback port (`127.0.0.1:18080`); `MainActivity` loads
   that origin with retries and `android:usesCleartextTraffic="true"`.
5. **Inset the WebView below the system bars.** A rendered page is not proof
   that the top chrome is reachable. Use `android:fitsSystemWindows="true"` or
   explicit `WindowInsets` padding; the spike's bottom dock worked while the
   hamburger/search were dead until the inset.
6. **The c-shared object needs Android libc shims.** `bcmp`,
   `__errno_location` and `__xpg_strerror_r` are not exported by Android's
   libc; provide them in an `_android.c` file plus a cgo-enabling Go file.
7. **The dock stays on Chat.** The mobile dock is rendered by `ui.Shell` on
   every module; Chat lifts its composer above the bar instead of hiding the
   dock. Do not reintroduce a `body[data-module="chat"] .app-dock { display:none }`.

## Validation

- `wails3 doctor` green.
- `adb install` succeeds; `am start` keeps the process alive.
- The landing page, Todo page and dock render; a todo created through
  `http://127.0.0.1:18080/api/todos` appears in the list.
- `adb logcat` has no `panic` / `UnsatisfiedLinkError`.
- Screenshots: `docs/research/android-spike-2026-10-*.png`.

## Limits (say them out loud)

Open auth, a fixed port, cleartext HTTP, no Keystore/Keychain, no deep-link
activation, no TLS and no CI job. A product release needs all of those; this
target is a measured path, not a support claim.
