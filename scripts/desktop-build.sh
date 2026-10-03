#!/usr/bin/env bash
# desktop-build.sh — Build the gogogo-fullstack-template as a native
# desktop app (Wails v3), Android APK, macOS .app, or CROSS-PLATFORM
# preview binaries via the wails-cross Docker image (Zig + macOS SDK).
#
# Usage (fácil p/ humano ou LLM — um entrypoint só):
#   ./scripts/desktop-build.sh                    # native (host OS/arch)
#   ./scripts/desktop-build.sh cross-windows [arch]  # Windows .exe (default amd64)
#   ./scripts/desktop-build.sh cross-darwin [arch]   # macOS (default arm64, NÃO assinado)
#   ./scripts/desktop-build.sh cross-linux [arch]    # Linux (default amd64)
#   ./scripts/desktop-build.sh cross-universal    # macOS universal (amd64+arm64 via lipo)
#   ./scripts/desktop-build.sh cross GOOS [GOARCH]   # genérico: cross darwin arm64
#   ./scripts/desktop-build.sh android             # Android APK
#   ./scripts/desktop-build.sh package            # macOS .app (só no macOS)
#
# Onde faz sentido usar cross (wails-cross):
#   - preview/teste das 3 plataformas a partir de UMA máquina Linux/macOS.
#   - PR preview e smoke sem pagar 3 runners nativos.
# Onde NÃO faz sentido (use runner nativo):
#   - release final macOS: cross não assina/notariza (Gatekeeper bloqueia).
#     Assine num runner macOS. Idem p/ .msi/.AppImage de distribuição.
#   - `make desktop` (go build puro) continua sendo o gate rápido local/CI.
#
# CGO: este repo PRECISA de CGO (ncruces/go-sqlite3 + WebView), então até
# o alvo Windows usa Docker no cross. Só o build nativo usa toolchain local.
#
# One-time setup (só p/ cross, ~800MB) — o wails3 CLI NÃO é mais necessário:
#   git clone https://github.com/wailsapp/wails && cd wails
#   docker build -t wails-cross -f build/docker/Dockerfile.cross build/docker/
#
# Build output:
#   build/desktop/              — native binary
#   build/cross/<goos>-<goarch>/ — previews cross (ex: build/cross/darwin-arm64/)
#   build/android/              — .apk (quando target é android)
#   build/package/              — .app bundle (macOS, quando target é package)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
OUTPUT_DIR="$PROJECT_DIR/build"
APP_NAME="${APP_NAME:-gogogo-fullstack-template}"
TARGET="${1:-native}"

# ── Color helpers ──
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color
# NOTE: printf '%s' (not `echo -e "$1"`) so escape sequences inside a message
# are never re-interpreted. Separately, never write backticks inside a
# double-quoted argument at the CALL site — the shell runs them as command
# substitution before the function is even entered (that is how an error
# message once executed `make desktop` and `wails3 init`). Quote command
# names with single quotes instead: 'wails3 package'.
info()  { printf '\033[0;32m→\033[0m %s\n' "$1"; }
warn()  { printf '\033[1;33m⚠\033[0m %s\n' "$1"; }
error() { printf '\033[0;31m✗\033[0m %s\n' "$1" >&2; }

# ── Prerequisite checks ──
check_go() {
    if ! command -v go &>/dev/null; then
        error "Go is not installed. Install Go 1.27+ from https://go.dev/dl/"
        return 1
    fi
    local version
    # Portable (BSD + GNU): `grep -oP` não existe no macOS.
    version=$(go version | sed -E 's/.*go([0-9]+\.[0-9]+).*/\1/')
    if [ -z "$version" ]; then
        warn "Could not parse Go version; skipping minimum-version check."
        return 0
    fi
    if ! awk -v v="$version" 'BEGIN { split(v, a, "."); exit !(a[1] > 1 || (a[1] == 1 && a[2] >= 27)) }'; then
        error "Go $version detected. Go 1.27+ required."
        return 1
    fi
    info "Go $version detected"
}

# ── Cross (wails-cross) helpers ──
# Só o path cross usa Docker. Native/android/package não tocam nisso.
check_docker() {
    if ! command -v docker &>/dev/null; then
        error "Docker not found. Cross builds need Docker (wails-cross image)."
        error "Install: https://docs.docker.com/get-docker/"
        return 1
    fi
    if ! docker info &>/dev/null; then
        error "Docker daemon not running. Start Docker Desktop or: sudo systemctl start docker"
        return 1
    fi
    info "Docker daemon OK"
}

ensure_cross_image() {
    if docker image inspect wails-cross &>/dev/null; then
        info "wails-cross image present"
        return 0
    fi
    error "wails-cross image missing (~800MB) and cannot be built automatically."
    error "'wails3 task setup:docker' needs a Taskfile.yml at the repo root, and"
    error "this repo has none. Build the image from the wails source checkout:"
    error "  git clone https://github.com/wailsapp/wails && cd wails"
    error "  docker build -t wails-cross -f build/docker/Dockerfile.cross build/docker/"
    error "macOS SDK license: the image pulls wailsapp/macosx-sdks — review Apple's SDK terms."
    return 1
}

default_arch_for() {
    case "$1" in
        darwin) echo "arm64" ;;
        windows) echo "amd64" ;;
        linux) echo "amd64" ;;
        *) echo "arm64" ;;
    esac
}

check_android() {
    error "Android builds are not wired in this repo — see build_android()."
    return 1
}

check_macos_package() {
    error "macOS .app packaging is not wired in this repo — see build_package()."
    return 1
}

# ── Build functions ──
build_native() {
    info "Building desktop binary for $(go env GOOS)/$(go env GOARCH)..."
    RESULT_DIR="$OUTPUT_DIR/desktop" # consumed by the final "Build complete!" echo
    mkdir -p "$RESULT_DIR"
    cd "$PROJECT_DIR"

    # First generate Templ components
    go tool templ generate

    # Plain `go build` — the same path `make desktop` uses, and the only one
    # that works here. The Wails v3 CLI is NOT required to compile a desktop
    # binary (the shell is just a Go program importing pkg/application), and
    # `wails3 build` cannot be used in this repo anyway:
    #   - it takes no `-o` flag (real flags: -tags, -obfuscated, -garbleargs,
    #     -nocolour), so the output path must come from wails.json;
    #   - it delegates to `wails3 task build`, which needs a Taskfile.yml at
    #     the repo root, and this repo has none (make is the task runner).
    # Using `go build` also keeps the native path free of any wails3-on-PATH
    # dependency, so the build can never depend on which CLI happens to be
    # installed.
    go build -o "$RESULT_DIR/$APP_NAME" ./cmd/desktop
    info "Binary: $OUTPUT_DIR/desktop/$APP_NAME"
}

build_android() {
    error "Android packaging is not wired in this repo."
    error "'wails3 android:package' does not exist in the pinned CLI (v3.0.0-beta.24,"
    error "which only ships 'android:overlay:gen'), and 'wails3 build/package'"
    error "requires a Taskfile.yml at the repo root that this repo does not have."
    error "Tracked as future work — see the note in .github/workflows/desktop.yml."
    return 1
}

build_ios() {
    info "iOS build not yet supported in this template."
    info "Wails v3 iOS support is experimental. Track: https://github.com/wailsapp/wails"
    return 1
}

build_package() {
    error "macOS .app packaging is not wired in this repo."
    error "'wails3 package' takes no '-o' flag (real flags: -nocolour only) and"
    error "delegates to 'wails3 task package', which needs a Taskfile.yml at the"
    error "repo root that this repo does not have. Use 'make desktop' (plain"
    error "'go build') for a runnable binary, or 'wails3 init' to scaffold a"
    error "Taskfile first if you want the full packaging pipeline."
    return 1
}

# Cross-preview via wails-cross (Zig + macOS SDK no Docker).
# Uso: build_cross <goos> [goarch]. Default arch por OS (darwin=arm64,
# windows/linux=amd64). Saída: build/cross/<goos>-<goarch>/.
# Binários darwin saem NÃO assinados — só p/ teste local, não distribua.
build_cross() {
    local goos="${1:?usage: build_cross GOOS [GOARCH]}"
    local goarch="${2:-$(default_arch_for "$goos")}"
    case "$goos" in
        darwin|windows|linux) ;;
        *) error "Unsupported GOOS: $goos (want darwin|windows|linux)"; return 1 ;;
    esac
    case "$goarch" in
        amd64|arm64) ;;
        *) error "Unsupported GOARCH: $goarch (want amd64|arm64)"; return 1 ;;
    esac

    check_docker || return 1
    ensure_cross_image || return 1

    local outdir="$OUTPUT_DIR/cross/${goos}-${goarch}"
    RESULT_DIR="$outdir" # consumed by the final "Build complete!" echo
    info "Cross-building for ${goos}/${goarch} (wails-cross, CGO via Zig)..."
    mkdir -p "$outdir"
    cd "$PROJECT_DIR"

    go tool templ generate
    # Direct docker invocation — same command wails3's own darwin/windows/linux
    # Taskfiles run, but without needing a Taskfile in this repo. The image
    # contract: `wails-cross <goos> <goarch>`, source mounted at /app, artifact
    # written to bin/<APP_NAME>-<goos>-<goarch>.
    docker run --rm -v "$PROJECT_DIR:/app" -e APP_NAME="$APP_NAME" \
        wails-cross "$goos" "$goarch"
    # The container runs as root; hand the artifact back to the host user.
    docker run --rm -v "$PROJECT_DIR:/app" alpine \
        chown -R "$(id -u):$(id -g)" /app/bin
    mkdir -p "$outdir"
    mv "$PROJECT_DIR/bin/${APP_NAME}-${goos}-${goarch}" "${outdir}/${APP_NAME}"
    info "Binary: ${outdir}/${APP_NAME}"
    if [ "$goos" = "darwin" ]; then
        warn "macOS cross binary is UNSIGNED. Sign on macOS before distributing."
    fi
}

build_cross_universal() {
    error "macOS universal packaging is not wired in this repo."
    error "It needs 'wails3 task darwin:build:universal', and this repo has no"
    error "Taskfile.yml at the root (make is the task runner here). Build both"
    error "arches individually instead:"
    error "  ./scripts/desktop-build.sh cross-darwin arm64"
    error "  ./scripts/desktop-build.sh cross-darwin amd64"
    error "then combine with: wails3 tool lipo"
    return 1
}

# ── Main ──
echo ""
echo "╔══════════════════════════════════════════╗"
echo "║  gogogo — Desktop Build Script          ║"
echo "╚══════════════════════════════════════════╝"
echo ""

check_go || exit 1

case "$TARGET" in
    native)
        # No wails3 needed: build_native uses plain `go build`.
        build_native
        ;;
    cross-windows)
        # $2 opcional: arch (default amd64). Ex: ./scripts/desktop-build.sh cross-windows arm64
        build_cross windows "${2:-$(default_arch_for windows)}"
        ;;
    cross-darwin)
        # $2 opcional: arch (default arm64)
        build_cross darwin "${2:-$(default_arch_for darwin)}"
        ;;
    cross-linux)
        # $2 opcional: arch (default amd64)
        build_cross linux "${2:-$(default_arch_for linux)}"
        ;;
    cross-universal)
        build_cross_universal
        ;;
    cross)
        # Genérico: ./scripts/desktop-build.sh cross <goos> [goarch]
        # Ex (LLM-friendly): ./scripts/desktop-build.sh cross darwin arm64
        if [ $# -lt 2 ]; then
            error "Usage: $0 cross <darwin|windows|linux> [amd64|arm64]"
            exit 1
        fi
        build_cross "$2" "${3:-$(default_arch_for "$2")}"
        ;;
    android)
        build_android
        ;;
    ios)
        build_ios
        ;;
    package)
        build_package
        ;;
    *)
        echo "Usage: $0 [native|cross-windows|cross-darwin|cross-linux|cross-universal|cross GOOS [GOARCH]|android|package]"
        echo ""
        echo "  native            — build for current OS/arch (default)"
        echo "  cross-windows [a] — Windows preview via wails-cross (default amd64)"
        echo "  cross-darwin [a]  — macOS preview via wails-cross (default arm64, UNSIGNED)"
        echo "  cross-linux [a]   — Linux preview via wails-cross (default amd64)"
        echo "  cross-universal   — macOS universal (amd64+arm64, UNSIGNED)"
        echo "  cross GOOS [ARCH] — generic (ex: $0 cross darwin arm64)"
        echo "  android           — build Android APK (requires SDK + NDK + JDK 21)"
        echo "  package           — build macOS .app bundle (macOS only)"
        exit 1
        ;;
esac

echo ""
info "Build complete! Output in: ${RESULT_DIR:-$OUTPUT_DIR/$TARGET}"
