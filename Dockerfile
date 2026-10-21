# syntax=docker/dockerfile:1.7
#
# Multi-stage build (maximo lean):
#   1. builder  — golang:1.27.3-alpine + nodejs/npm. Runs the Tailwind v4
#                  + DaisyUI v5 CSS build, generates Templ, compiles
#                  the Go binary (CGO_ENABLED=1, dynamically linked
#                  against musl, stripped).
#   2. runtime  — alpine. The runtime image contains ONLY:
#                    - /app         (the Go binary, dynamically linked)
#                    - ca-certificates + libgcc (apk; HTTPS to LLM
#                      providers, and the tursogo musl library's NEEDED)
#                    - /tmp         (writable; Go runtime expects it)
#                  No shell beyond busybox, no /etc/passwd.
#
# Alpine and NOT scratch: the flat stack's database driver (tursogo)
# loads a Rust shared library via dlopen at first DB open, which needs
# musl's dynamic loader. A static binary on scratch panics with
# `Dynamic loading not supported` — that exact shape crash-looped
# production on the 0.44.0 migration. See deploy/Dockerfile.prod,
# which carries the same constraint plus the CI boot-smoke gate
# (scripts/smoke-prod-image.sh). Do NOT move either image back to
# scratch without re-proving the boot.
#
# Build pipeline (Makefile 'css' target) + this Dockerfile are the
# only two compile steps. No JS runtime in the final image.

# ────────────────────────────
# Stage 1: builder
# ────────────────────────────
FROM golang:1.27.3-alpine AS builder
RUN apk add --no-cache git ca-certificates nodejs npm gcc musl-dev
# ARGs for build-time metadata injection. Defaults match local
# builds; the docker-image Makefile target passes real values via
# --build-arg (see Makefile).
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILDTIME=

WORKDIR /src

# Install Go module deps first (cached independently of source).
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    go mod download

# Install CSS build deps (Tailwind v4 CLI + DaisyUI v5).
COPY package.json package-lock.json* ./
RUN --mount=type=cache,target=/root/.npm,sharing=locked \
    npm install --no-audit --no-fund

# Now copy the rest of the source and run all the build steps.
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    --mount=type=cache,target=/root/.npm,sharing=locked \
    go tool templ generate && \
    npm run build --silent && \
    npx tailwindcss -i ./src/css/basecoat-input.css -o ./web/resources/static/basecoat.min.css --silent && \
    # Inject build metadata (Version/CommitHash/BuildTime) so the
    # navbar version badge reflects exactly what was built. ARG
    # VERSION/COMMIT/BUILDTIME are supplied by docker buildx (default
    # 'dev' / 'unknown' / '' when invoked locally). Pass them via
    # `docker buildx build --build-arg VERSION=v0.21.2 --build-arg
    # COMMIT=$(git rev-parse --short HEAD)`.
    #
    # -tags musl + no -static: tursogo dlopens its musl-linked library
    # at first DB open, which needs a dynamically linked binary and a
    # musl runtime (see deploy/Dockerfile.prod). A static binary on
    # scratch dies with "Dynamic loading not supported" (prod 0.44.0).
    ARG VERSION=dev
    ARG COMMIT=unknown
    ARG BUILDTIME=
    CGO_ENABLED=1 go build -tags musl -trimpath -ldflags="-s -w -X main.Version=${VERSION} -X main.CommitHash=${COMMIT} -X main.BuildTime=${BUILDTIME}" -o /out/app ./cmd/web/

# ────────────────────────────
# Stage 2: runtime (alpine)
# ────────────────────────────
# Alpine, not scratch: the app dlopens the tursogo Rust library at
# first DB open, which needs musl's dynamic loader plus libgcc_s.
# ca-certificates for HTTPS to LLM providers (OpenAI, Groq, etc.).
FROM alpine:3.22
RUN apk add --no-cache ca-certificates libgcc

# The dynamically-linked Go binary (CGO_ENABLED=1, musl).
COPY --from=builder /out/app /app

# Alpine ships a writable /tmp, which the Go runtime expects (and
# which backs $HOME/.cache when TURSO_GO_CACHE_DIR is unset).

# The Go runtime expects a writable HOME for some operations. Set it to /tmp.
ENV HOME=/tmp \
    XDG_CONFIG_HOME=/tmp \
    XDG_CACHE_HOME=/tmp

# The credits plugin reads its encryption key from an env var
# (CREDITS_ENC_KEY); operators set secrets via the age-encrypted workflow.
# See bin/init-secrets for the age-encrypted workflow.
# ENCRYPTION_ENV=

# Default data dir: write to /tmp/data so the container
# is stateless. For a real deployment, mount a volume here.
ENV DATA_DIR=/tmp/data

# Run as non-root: numeric UID works without an /etc/passwd entry.
# 65532 is the standard "nonroot" UID used by distroless images.
USER 65532:65532

EXPOSE 8080

# By default, a Go program is PID 1 and receives signals directly
# (no shell wrapping). Probe GET /health directly (k8s livenessProbe,
# ECS health check, or `/app health`, which GETs it internally).
ENTRYPOINT ["/app"]
