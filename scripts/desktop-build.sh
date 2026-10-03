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
# One-time setup (só p/ cross, ~800MB):
#   wails3 task setup:docker   # o script roda sozinho se a imagem faltar
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
# Keep CLI pinned to go.mod (wails/v3 v3.0.0-beta.24). Bump together.
WAILS_VERSION="${WAILS_VERSION:-v3.0.0-beta.24}"
WAILS_PKG="github.com/wailsapp/wails/v3/cmd/wails3@${WAILS_VERSION}"

# ── Color helpers ──
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color
info()  { echo -e "${GREEN}→${NC} $1"; }
warn()  { echo -e "${YELLOW}⚠${NC} $1"; }
error() { echo -e "${RED}✗${NC} $1"; }

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

check_wails() {
    # GOPATH/bin may not be on PATH (fresh `go install`); ensure it is so
    # both a pre-existing and a just-installed wails3 are found below.
    case ":$PATH:" in
        *":$(go env GOPATH)/bin:"*) ;;
        *) export PATH="$PATH:$(go env GOPATH)/bin" ;;
    esac
    if ! command -v wails3 &>/dev/null && ! command -v wails &>/dev/null; then
        warn "Wails CLI not found. Installing ${WAILS_PKG}..."
        go install "${WAILS_PKG}"
        if ! command -v wails3 &>/dev/null; then
            error "Wails installation failed. Try: go install ${WAILS_PKG}"
            return 1
        fi
    fi
    local cmd
    cmd=$(command -v wails3 2>/dev/null || command -v wails 2>/dev/null)
    info "Wails CLI: $cmd"
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
    # wails3 detecta a imagem sozinho; só garantimos que ela existe.
    if docker image inspect wails-cross &>/dev/null; then
        info "wails-cross image present"
        return 0
    fi
    warn "wails-cross image missing (~800MB download). Running one-time setup..."
    warn "macOS SDK license: image pulls from wailsapp/macosx-sdks — review Apple's SDK terms."
    wails3 task setup:docker || {
        error "setup:docker failed. Run manually: wails3 task setup:docker"
        return 1
    }
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
    if [ -z "${ANDROID_HOME:-}" ] && [ -z "${ANDROID_SDK_ROOT:-}" ]; then
        error "ANDROID_HOME or ANDROID_SDK_ROOT not set."
        error "Install Android SDK + NDK, then: export ANDROID_HOME=~/Android/Sdk"
        return 1
    fi
    local sdk="${ANDROID_HOME:-$ANDROID_SDK_ROOT}"
    if [ ! -d "$sdk" ]; then
        error "Android SDK directory not found: $sdk"
        return 1
    fi
    info "Android SDK: $sdk"

    if ! command -v java &>/dev/null; then
        error "Java (JDK 21) not found. Install via: brew install openjdk@21"
        return 1
    fi
    local java_version
    java_version=$(java -version 2>&1 | grep -oP 'version "\K[0-9]+')
    if [ "$java_version" -lt 21 ]; then
        error "Java $java_version detected. JDK 21+ required."
        return 1
    fi
    info "Java $java_version detected"
}

check_macos_package() {
    if [ "$(uname)" != "Darwin" ]; then
        error "macOS .app packaging is only available on macOS."
        return 1
    fi
    if ! xcode-select -p &>/dev/null; then
        error "Xcode Command Line Tools not installed. Run: xcode-select --install"
        return 1
    fi
    info "Xcode Command Line Tools detected"
}

# ── Build functions ──
build_native() {
    info "Building desktop binary for $(go env GOOS)/$(go env GOARCH)..."
    RESULT_DIR="$OUTPUT_DIR/desktop" # consumed by the final "Build complete!" echo
    mkdir -p "$RESULT_DIR"
    cd "$PROJECT_DIR"

    # First generate Templ components
    go tool templ generate

    # Build with Wails or plain Go
    if command -v wails3 &>/dev/null; then
        wails3 build -o "$RESULT_DIR/$APP_NAME"
    elif command -v wails &>/dev/null; then
        wails build -o "$RESULT_DIR/$APP_NAME"
    else
        go build -o "$RESULT_DIR/$APP_NAME" ./cmd/desktop
    fi
    info "Binary: $OUTPUT_DIR/desktop/$APP_NAME"
}

build_android() {
    info "Building Android APK..."
    check_android || return 1
    mkdir -p "$OUTPUT_DIR/android"
    cd "$PROJECT_DIR"

    go tool templ generate
    wails3 android:package -o "$OUTPUT_DIR/android/$APP_NAME.apk"
    info "APK: $OUTPUT_DIR/android/$APP_NAME.apk"
}

build_ios() {
    info "iOS build not yet supported in this template."
    info "Wails v3 iOS support is experimental. Track: https://github.com/wailsapp/wails"
    return 1
}

build_package() {
    info "Building macOS .app bundle..."
    check_macos_package || return 1
    mkdir -p "$OUTPUT_DIR/package"
    cd "$PROJECT_DIR"

    go tool templ generate
    wails3 package GOOS=darwin -o "$OUTPUT_DIR/package"
    info ".app bundle: $OUTPUT_DIR/package"
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

    check_wails || return 1
    check_docker || return 1
    ensure_cross_image || return 1

    local outdir="$OUTPUT_DIR/cross/${goos}-${goarch}"
    RESULT_DIR="$outdir" # consumed by the final "Build complete!" echo
    info "Cross-building for ${goos}/${goarch} (wails-cross, CGO via Zig)..."
    mkdir -p "$outdir"
    cd "$PROJECT_DIR"

    go tool templ generate
    # O Taskfile do wails3 detecta host!=target e usa o container sozinho.
    wails3 build "GOOS=${goos}" "GOARCH=${goarch}" -o "${outdir}/${APP_NAME}"
    info "Binary: ${outdir}/${APP_NAME}"
    if [ "$goos" = "darwin" ]; then
        warn "macOS cross binary is UNSIGNED. Sign on macOS before distributing."
    fi
}

build_cross_universal() {
    check_wails || return 1
    check_docker || return 1
    ensure_cross_image || return 1
    RESULT_DIR="$OUTPUT_DIR/cross/darwin-universal" # final echo; task may also write bin/
    mkdir -p "$RESULT_DIR"
    cd "$PROJECT_DIR"

    go tool templ generate
    # Combina amd64+arm64 via `wails3 tool lipo` interno (funciona fora do macOS).
    wails3 task darwin:build:universal
    info "Universal binary in: $OUTPUT_DIR/cross/darwin-universal (ou bin/ — ver output acima)"
    warn "macOS cross binary is UNSIGNED. Sign on macOS before distributing."
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
        check_wails || true  # wails is optional for native build
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
        check_wails
        build_android
        ;;
    ios)
        build_ios
        ;;
    package)
        check_wails
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
