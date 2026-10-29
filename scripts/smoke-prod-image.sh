#!/usr/bin/env bash
# smoke-prod-image.sh — boot the REAL production image and assert /health.
#
# Why this exists: nothing used to boot the final image. `go build` was green,
# the binary smoke test booted the host binary (glibc, real /tmp), but the
# scratch image had no dynamic loader — so tursogo's dlopen panicked on the
# first DB open and the container crash-looped with 502s. An image that only
# ever passed a build step reached production once; this is the gate that
# makes "the built artifact" a verified thing rather than an assumption.
#
# This script builds deploy/Dockerfile.prod, runs it like production
# (uid 65532, DATA_DIR on a mounted volume, TURSO_GO_CACHE_DIR on that
# volume), polls /health until 200, then tears the container down.
# CI runs it after the browser smokes; run it locally after any change
# to deploy/Dockerfile.prod, deploy/docker-compose.prod.yml, internal/db,
# or the tursogo pin in go.mod.
#
# Usage: bash scripts/smoke-prod-image.sh [IMAGE_TAG]
#   IMAGE_TAG defaults to gogogo:prod-smoke. The image is left tagged
#   locally for inspection; the container is always removed (trap).
set -euo pipefail

TAG="${1:-gogogo:prod-smoke}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DATA_DIR="$(mktemp -d "${TMPDIR:-/tmp}/gogogo-prod-smoke-XXXXXX")"
CTR="gogogo-prod-smoke-$$"

cleanup() {
  docker rm -f "$CTR" >/dev/null 2>&1 || true
  # Files under DATA_DIR belong to container uid 65532 (the app ran as
  # non-root), so a plain rm -rf fails with Permission denied. Wipe via a
  # throwaway root container on the just-built image (alpine has rm), then
  # remove the husk. All best-effort: cleanup must never fail the gate.
  docker run --rm --user 0 --entrypoint rm -v "$DATA_DIR:/d" "$TAG" -rf /d >/dev/null 2>&1 || true
  rm -rf "$DATA_DIR" 2>/dev/null || true
}
trap cleanup EXIT

# Writable for container uid 65532 (we may not own the dir as 65532 and
# cannot chown without root — same fallback as scripts/deploy-prod.sh).
chmod 0777 "$DATA_DIR"

echo "→ building $TAG from $ROOT/deploy/Dockerfile.prod"
BTIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
if [ "${SMOKE_BUILDX_CACHE:-0}" = "1" ]; then
	# CI path: buildx + the Actions layer cache, so an unchanged layer set is
	# not rebuilt. The Go build inside the Dockerfile is the expensive layer and
	# plain `docker build` recompiled it on every run. --load puts the result in
	# the local daemon, which is what the boot check below runs. Locally this
	# stays off: it needs a buildx builder that only the workflow sets up.
	docker buildx build \
		--cache-from type=gha,scope=prod-smoke \
		--cache-to type=gha,mode=max,scope=prod-smoke \
		--load \
		-f "$ROOT/deploy/Dockerfile.prod" \
		--build-arg VERSION=smoke \
		--build-arg COMMIT=smoke \
		--build-arg BUILDTIME="$BTIME" \
		-t "$TAG" "$ROOT"
else
	docker build -f "$ROOT/deploy/Dockerfile.prod" \
		--build-arg VERSION=smoke \
		--build-arg COMMIT=smoke \
		--build-arg BUILDTIME="$BTIME" \
		-t "$TAG" "$ROOT"
fi

# Ephemeral host port: a fixed one collides with a dev server or a
# parallel run. Ask the kernel via python (present on CI + server).
PORT="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])')"

echo "→ booting $CTR on 127.0.0.1:$PORT (data: $DATA_DIR)"
docker run -d --rm --name "$CTR" \
  --user "65532:65532" \
  -p "127.0.0.1:${PORT}:8080" \
  -e HOST=0.0.0.0 \
  -e PORT=8080 \
  -e ENVIRONMENT=production \
  -e DATA_DIR=/var/lib/gogogo/data \
  -e TURSO_GO_CACHE_DIR=/var/lib/gogogo/data/.turso-cache \
  -e NATS_ENABLED=false \
  -e DAGNATS_ENABLED=false \
  -v "$DATA_DIR:/var/lib/gogogo/data" \
  "$TAG" >/dev/null

# The realtime/NATS layers stay off (same as the binary smoke test in
# cmd/web): this gate owns the DB-open path that regressed, not the
# broadcaster. NATS/DAGNATS ride the same boot after BootChi returns.

echo "→ polling /health (120s budget)"
deadline=$((SECONDS + 120))
while true; do
  if out="$(curl -fsS -o /dev/null -w '%{http_code}' "http://127.0.0.1:${PORT}/health" 2>/dev/null)"; then
    if [ "$out" = "200" ]; then
      echo "✓ /health → 200"
      break
    fi
  fi
  # Surface the crash-loop early instead of polling a dead container
  # for two minutes: a non-running container means boot panicked.
  if [ "$(docker inspect -f '{{.State.Running}}' "$CTR" 2>/dev/null || echo false)" != "true" ]; then
    echo "❌ container exited during boot. Logs:"
    docker logs "$CTR" 2>&1 | tail -30
    exit 1
  fi
  if [ $SECONDS -ge $deadline ]; then
    echo "❌ /health never returned 200 within 120s. Logs:"
    docker logs "$CTR" 2>&1 | tail -30
    exit 1
  fi
  sleep 2
done

BUILD_HDR="$(curl -fsS -D - -o /dev/null "http://127.0.0.1:${PORT}/health" | tr -d '\r' | grep -i '^x-gogogo-build:' || true)"
echo "✓ ${BUILD_HDR:-'(no X-Gogogo-Build header — non-fatal)'}"
echo "✅ prod-image smoke passed ($TAG boots and serves /health)"
