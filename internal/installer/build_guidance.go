// Installer engine: build-target guidance shared by the CLI
// help and the MCP `build_prerequisites` tool.
package installer

// DesktopPrerequisites is the desktop half of the build guidance.
const DesktopPrerequisites = `Desktop (local, opt-in; no CI compiles it):
  1. Prereqs: macOS Xcode CLT; Linux GTK4 + WebKitGTK; Windows WebView2 + a C
     toolchain.
  2. Build: make desktop -> build/desktop/gogogo-desktop.
  3. Gate: make desktop-smoke (window + webview + first-run signup + single
     instance + activation gate). make desktop only compiles.
  4. The shell boots StartChi on loopback, clears Secure cookies, takes the
     single-instance lock, and applies LICENSE_MODE.
  Recipe: docs/desktop-mobile.md + references/desktop-native.md`

// GuiPrerequisites is the gogpu/ui half of the build guidance.
const GuiPrerequisites = `Native window (local, opt-in; no CI compiles it):
  1. Prereqs: none — pure Go (CGO_ENABLED=0), no system headers.
  2. Build: make gui (race tests on cmd/gui, then CGO_ENABLED=0 build)
     -> build/gui/gogogo-gui.
  3. Gate: make gui itself is the gate (vet+tests+build). Lint is manual:
     make lint-gui. Run the window with make run-gui.
  4. The window calls features/todo/service directly (no HTTP, no cookie);
     online-only, no outbox yet.
  Recipe: docs/desktop-mobile.md#native-window-gogpui + references/desktop-native.md`

// MygoPrerequisites is the MyGo half of the build guidance.
const MygoPrerequisites = `MyGo shell (local, opt-in; no CI compiles it):
  1. Prereqs: same as desktop (macOS Xcode CLT / Linux GTK+WebKit) — CGO on
     for the tursogo link. The mygo CLI is packaging only (warn in doctor);
     make mygo needs no CLI.
  2. Build: make mygo -> build/mygo/gogogo-mygo. Gate: make mygo-smoke
     (window + webview + first-run signup + single-instance). make mygo
     only compiles. Packaging: go tool github.com/egoist/mygo/cmd/mygo
     build ./cmd/mygo (reads cmd/mygo/mygo.json).
  3. The shell boots StartChi on loopback, clears Secure cookies, takes the
     MyGo single-instance lock (app-wide), opens the window at
     desktopboot.WebviewURL, and applies LICENSE_MODE. Proof:
     ENVIRONMENT=dev + isolated DB -> cookie persists, GET /api/todos 200,
     SSE patch. Source-only (not in install.sh archives, like desktop).
  Recipe: docs/desktop-mobile.md#mygo-shell-peer-shell-over-the-flat-stack +
     docs/research/mygo-2026-11.md + references/desktop-native.md`

// MobilePrerequisites is the Android half of the build guidance.
const MobilePrerequisites = `Mobile Android (local, opt-in; no CI compiles it):
  1. Prereqs: Android SDK/NDK 27.2.12479018, platform 35, build-tools 35,
     JDK 21, Wails CLI v3.0.0-beta.24, an API 35 arm64 emulator.
  2. Build: make mobile (wails3 task android:build + android:assemble:apk)
     -> bin/gogogo.apk; adb install -r bin/gogogo.apk.
  3. Entrypoint: cmd/mobile. Android swaps the driver to ncruces for
     internal/db, internal/queue and the credits ledger (tursogo has no
     Android library).
  4. WebView: fixed loopback 127.0.0.1:18080, cleartext, retries, system-bar
     insets. The Wails asset origin is Java-side and cannot proxy to Go.
  5. Limits: open auth, fixed port, no Keystore/deep-link/TLS; not a support
     claim. Full recipe: references/mobile-android.md
  Docs: docs/desktop-mobile.md#mobile-android-opt-in-target-not-supported`
