# Android spike (2026-10-30)

**Verdict: not supported.** The APK builds, installs and launches on the
emulator, but the WebView never reaches the app: the activity loads
`https://wails.localhost/` and gets `ERR_CONNECTION_REFUSED`. No product claim
follows from this branch; `docs/desktop-mobile.md` still says mobile is
"prepared, not wired".

## Environment installed

| What | Version / path |
|---|---|
| Android command-line tools | `brew install --cask android-commandlinetools` → `/opt/homebrew/share/android-commandlinetools` |
| JDK | `brew install openjdk@21` → `/opt/homebrew/opt/openjdk@21` (21.0.13) |
| SDK packages | `platform-tools`, `platforms;android-35`, `build-tools;35.0.0`, `ndk;27.2.12479018`, `emulator`, `system-images;android-35;google_apis;arm64-v8a` |
| AVD | `gogogo-api35` (Pixel 7, arm64-v8a), booted headless with `-no-window -no-audio -no-boot-anim -gpu swiftshader_indirect` |
| Wails CLI | v3.0.0-beta.24; `wails3 doctor` reports "ready" with the SDK/JDK set |

## Commands that built the APK

```bash
export ANDROID_HOME=/opt/homebrew/share/android-commandlinetools
export ANDROID_SDK_ROOT=$ANDROID_HOME
export ANDROID_NDK_HOME=$ANDROID_HOME/ndk/27.2.12479018
export JAVA_HOME=/opt/homebrew/opt/openjdk@21

wails3 task android:build        # compiles cmd/desktop → libwails.so (arm64)
wails3 task android:assemble:apk # Gradle assembleDebug → bin/gogogo.apk
adb install -r bin/gogogo.apk
adb shell am start -n com.wails.app/.MainActivity
```

`Taskfile.yml` and `build/android/` are the minimum Wails mobile project
(Taskfile + Gradle app). The Go build target is `./cmd/desktop`, with
`-tags android,debug`.

## Blocker (exact)

The first load failed before Go started because Android's libc does not export
symbols the Go c-shared object references:

```
java.lang.UnsatisfiedLinkError: dlopen failed: cannot locate symbol "bcmp"
  referenced by .../lib/arm64/libwails.so
```

`cmd/desktop/bcmp_android.c` adds the `bcmp`, `__errno_location` and
`__xpg_strerror_r` aliases; with those the library loads and the process stays
alive. The UI still does not:

```
E Wails: [JNI] timeout waiting for app to be ready, dropping request for /index.html
W WailsPathHandler: Asset not found: /index.html
D WailsActivity: Page loaded: https://wails.localhost/
```

Screenshot: `docs/research/android-spike-2026-10.png` (WebView error page).

Root cause: `cmd/desktop` is a desktop entrypoint. It boots the loopback HTTP
server and takes the Wails single-instance lock before `app.Run`, and it has no
Wails `Assets` handler. Wails Android expects a mobile entrypoint that
registers `application.RegisterAndroidMain` (the generated overlay does) and
serves the activity's `wails.localhost` origin; it does not point the WebView at
the process's loopback server. A real mobile shell needs its own `cmd/mobile`
entrypoint (asset handler or an explicit loopback URL), Keystore-backed tokens
and a deep-link activation flow — none of that is in this spike.

Also note: `purego`/`tursogo` pull glibc-shaped symbols into the shared object
(the shims above are the symptom). The database layer has not been verified on
Android.

## Disk usage and cleanup

Measured after the spike:

| Path | Size |
|---|---|
| `/opt/homebrew/share/android-commandlinetools` (SDK + NDK + emulator + system image) | 8.1 GB |
| `~/.android/avd/gogogo-api35.avd` | 2.0 GB |
| `bin/gogogo.apk` (generated, not committed) | 91 MB |

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
