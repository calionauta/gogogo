# Native targets: desktop (Wails) and window (gogpu/ui)

Two binaries sit outside the web build. Neither is compiled by any CI job, so a
change that breaks one fails **nowhere** until someone builds it. That makes the
build a discipline, not a formality: run the target before pushing.

- `cmd/desktop` — Wails v3 shell. Renders the real web UI in a webview over a
  reverse proxy to `internal/server.StartChi`. Gets every feature the web app
  has. Needs CGO and OS webview headers.
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
make run-desktop    # build, then open the window
make gui            # race tests, then → build/gui/gogogo-gui (CGO off)
make run-gui        # open the native window (needs a display + GPU)
make lint-gui       # manual full-lint pass on cmd/gui; never in CI
```

Output goes to `build/`, never the repo root: a stray 115 MB binary is how a
commit grows a payload nobody asked for.

## Rules

**Never run `go build ./...` or `go test ./...`** as a shortcut — both compile
`cmd/desktop` and fail without the platform headers. `make build` / `make test`
use `scripts/web-packages.sh`, which excludes the two targets.

**Touching `cmd/desktop` means running `make desktop`.** There is no runner to
catch a broken shell. Same for `cmd/gui` and `make gui`.

**The Wails pin lives in `go.mod`.** No Makefile variable, no workflow copy to
keep in sync.

**Native code inside `cmd/web` is still banned.** These are separate binaries;
see `exception-to-go.md` for what "native" is allowed to mean.

## Common failures

- **`ld: warning: object file built for newer 'macOS'...`** — harmless
  deployment-target noise from Wails' prebuilt objects. The binary runs.
- **`fatal error: gtk/gtk.h` not found** — the Linux prerequisites are missing;
  the build stopped at CGO, not at your change.
- **Window opens but the page is blank** — the flat stack did not come up. Check
  `PORT` and that nothing else holds it (the default is `:8080`).
- **`goffi` link error at `CGO_ENABLED=0`** — `cmd/gui` must not import
  `internal/db` (tursogo/purego). Use the `ncruces` driver, as `cmd/gui` does.
