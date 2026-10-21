#!/usr/bin/env bash
# deploy-prod.sh — runs ON the server, after the GitHub Action has scp'd the
# compose file into place. Idempotent: it converges to the deployed state no
# matter how many times it runs.
#
# Layout (managed by the deploy workflow):
#   /home/deploy/services/gogogo/
#     compose/docker-compose.prod.yml   (replaced on every deploy)
#     env/.env                  (committed to repo, no secrets)
#     repo/                     (clone at master — the image build context)
#     data/                     (persistent volume: app.db, queue.db,
#                                nats/, dagnats/; gitignored)
#   /home/deploy/.secrets/gogogo.env  (mode 600, rendered every deploy from
#                                   GH Secrets; the compose env_file points
#                                   here. Never committed.)
#
# We use the services/ dir under the home (not /opt/) because the deploy user
# does not have passwordless sudo; /opt is root-owned. services/ in the home is
# writable by the deploy user and Docker still reads the compose file and binds
# the volume from there.
#
# This script is the second half of the deploy: the GH Action brings the source
# clone to master and renders the secrets file; this script builds the image
# from that clone, restarts the container, and verifies what landed. There is
# no host-side binary — the container is the artifact, and a static binary
# could not run here anyway (the DB driver dlopens a musl library at first open).
#
# Inputs (all optional; the deploy workflow sets the first two):
#   APP_IMAGE         digest-pinned image to pull, e.g.
#                     <registry>/<owner>/<repo>@sha256:<digest>. WITHOUT a
#                     @sha256: digest the script builds the image locally from
#                     the clone instead (the offline/fallback path).
#   EXPECTED_COMMIT   commit the artifact was built from; the script refuses to
#                     report success unless /health's X-Gogogo-Build says so.
#   DEPLOY_PROD_DRY_RUN=1  print the plan (mode + commands) and exit without
#                     touching Docker. Used by the script's own test.
#
# Rollback: redeploy an earlier digest (docs/deploy.md#rollback).

set -euo pipefail

# PROJECT/APP_DIR are overridable so a renamed scaffold (and the script's own
# dry-run test) can point at a scratch tree; the deploy sets neither.
PROJECT="${PROJECT:-gogogo}"
APP_DIR="${APP_DIR:-/home/deploy/services/${PROJECT}}"
COMPOSE_DIR="${APP_DIR}/compose"
# Secrets: rendered by the deploy workflow at ~/.secrets/<app>.env, which is
# where deploy/docker-compose.prod.yml points its env_file. This used to be
# <APP_DIR>/secrets/<app>.env and the two sides disagreed after the rename;
# keep this path in sync with the compose file.
SECRETS_DIR="/home/deploy/.secrets"
SECRETS_FILE="${SECRETS_DIR}/${PROJECT}.env"
# Bind mount location for the app SQLite files. Must match
# the host path inside deploy/docker-compose.prod.yml.
#
# IMPORTANT: lives under the deploy user home (owned by deploy), NOT
# /var/lib. The deploy user is non-root and CANNOT mkdir/chown under
# /var/lib (root-owned) — that was the original bug: the script aborted
# at `chown 65532:65532 /var/lib/.../data` and the container then
# crashed with sqlite3: permission denied. Keeping the data dir inside
# the home lets the deploy user create it and grant the container
# (uid 65532) write access without root.
DATA_DIR="${DATA_DIR:-${APP_DIR}/data}"
COMPOSE_FILE="${COMPOSE_DIR}/docker-compose.prod.yml"

cd "${APP_DIR}"

# ── 0. Which artifact? ──
# One rule: the deploy names the artifact it verified. A digest is immutable; a
# tag is not, so a tag is refused instead of silently pulled — that is the
# difference between "promoted what CI built" and "pulled whatever moved".
IMAGE_REF="${APP_IMAGE:-}"
DEPLOY_MODE="build"
case "${IMAGE_REF}" in
    "") : ;;
    *"@sha256:"*) DEPLOY_MODE="pull" ;;
    *)
        echo "✗ APP_IMAGE=${IMAGE_REF} is not digest-pinned (no @sha256:)" >&2
        echo "  pass <registry>/<owner>/<repo>@sha256:<digest>, or leave APP_IMAGE" >&2
        echo "  empty to build the image from the clone instead." >&2
        exit 1
        ;;
esac
export APP_IMAGE="${IMAGE_REF}"
REGISTRY="${IMAGE_REF%%/*}"

# The workflow logs in before pulling, which writes a credential into
# ~/.docker/config.json. Clear it on the way out, success or failure: the
# server must not keep a registry credential between deploys.
trap 'docker logout "${REGISTRY}" >/dev/null 2>&1 || true' EXIT

if [ "${DEPLOY_PROD_DRY_RUN:-0}" = "1" ]; then
    echo "dry-run: mode=${DEPLOY_MODE}"
    echo "dry-run: image=${IMAGE_REF:-<built from clone>}"
    echo "dry-run: expected-commit=${EXPECTED_COMMIT:-<derived from clone>}"
    echo "dry-run: would run: docker compose pull|build + up -d --wait + assert"
    exit 0
fi

# ── 1. Compose file in place ──
# The GH Action scp'd docker-compose.prod.yml directly into COMPOSE_DIR.
# Sanity-check it.
if [ ! -f "${COMPOSE_FILE}" ]; then
    echo "❌ ${COMPOSE_FILE} missing. Aborting." >&2
    exit 1
fi

# ── 2. Secrets file mode + ownership ──
# GH Action wrote a .env file with real secrets (GOAI_API_KEY etc).
# Lock it down to 600, owned by deploy user, before any container
# can read it.
if [ -f "${SECRETS_FILE}" ]; then
    chmod 0600 "${SECRETS_FILE}"
    echo "→ Secrets file mode set to 0600"
fi

# ── 3. Data dir ownership (the app writes here) ──
# The compose file bind-mounts ${DATA_DIR} (host) onto
# /var/lib/${PROJECT}/data (container). The deploy user is non-root
# and CANNOT chown to an arbitrary UID (65532). We therefore grant the
# container write access a different way:
#   1. Try `sudo -n chown -R` only if passwordless sudo is configured
#      (harmless no-op otherwise).
#   2. Prefer an ACL granting uid 65532 rwx (no other users affected),
#      applied RECURSIVELY (-R) with a DEFAULT acl (-d) so subdirs the
#      app or the calling workflow pre-create also
#      grant uid 65532 rwx. Without this the container (running as
#      65532) cannot write its SQLite WAL files and crashes with
#      permission denied.
#   3. Fall back to world-writable (chmod -R 0777) if setfacl is absent.
mkdir -p "${DATA_DIR}"
if sudo -n true 2>/dev/null; then
    sudo -n chown -R 65532:65532 "${DATA_DIR}" 2>/dev/null || true
fi
if command -v setfacl >/dev/null 2>&1; then
    # Recurse so existing deploy-owned subdirs get
    # the ACL + default ACL (new files inherit 65532 rwx). Errors on
    # container-owned .db files are expected (deploy can't chown/chmod
    # them) and harmless — those files are already owned by 65532 and
    # therefore writable by the container. Never abort on them.
    setfacl -R -m u:65532:rwx -d -m u:65532:rwx "${DATA_DIR}" 2>/dev/null || true
else
    chmod -R 0777 "${DATA_DIR}" 2>/dev/null || true
fi
echo "→ Data dir ready: ${DATA_DIR} (container uid 65532 gets rwx via ACL or 0777)"

# ── 4. Free disk space ──
# The server accumulates old Docker images, build cache, and dangling layers.
# In build mode, prune aggressively (the clone can rebuild anything). In pull
# mode the previous images ARE the rollback path, so keep a week of them and
# drop only the build cache, which a pulling host never uses.
if [ "${DEPLOY_MODE}" = "pull" ]; then
    echo "→ Pruning build cache + images older than 7d (rollback keeps images)"
    docker image prune -af --filter "until=168h" 2>/dev/null || true
else
    echo "→ Pruning Docker system before build..."
    docker system prune -af --filter "until=24h" 2>/dev/null || true
fi
docker builder prune -af 2>/dev/null || true
echo "→ Disk after prune: $(df -h / | awk 'NR==2{print $4}') free"

# ── 5. Get the artifact ──
# Build context is the repo checkout (cloned/updated by the GH
# Action into ${APP_DIR}/repo). The compose file lives at
# deploy/docker-compose.prod.yml relative to the repo root.
REPO_DIR="${APP_DIR}/repo"
cd "${REPO_DIR}"
# Read the build metadata locally so the Dockerfile.prod ARG defaults are
# overridden with the real values, not "dev"/"unknown"/"". The same values are
# stamped into the binary, so /health's X-Gogogo-Build header names exactly
# which commit is serving.
BUILD_VERSION="$(git describe --tags --abbrev=0 2>/dev/null | sed "s/^v//" || echo dev)"
BUILD_COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

if [ "${DEPLOY_MODE}" = "pull" ]; then
    echo "→ pulling ${IMAGE_REF} (digest pinned; nothing is compiled here)"
    docker compose -f deploy/docker-compose.prod.yml pull --quiet "${PROJECT}"
else
    echo "→ building the image from ${REPO_DIR} (no digest supplied)"
    docker compose -f deploy/docker-compose.prod.yml build "${PROJECT}" \
        --build-arg "VERSION=${BUILD_VERSION}" \
        --build-arg "COMMIT=${BUILD_COMMIT}" \
        --build-arg "BUILDTIME=${BUILD_TIME}"
fi

# Captured before the roll so the verification step can tell whether the
# container was recreated by THIS run or is a survivor from an earlier one.
ROLL_START_EPOCH="$(date -u +%s)"

# ── 6. Roll the container ──
# Without blue-green infra, the brief (2-5s) gap between stopping the
# old container and the new one responding makes the tunnel return a
# Bad Gateway. `--wait` blocks until the compose healthcheck passes,
# which narrows that window. A proper blue-green swap (start the new
# container on a secondary port, healthcheck it, flip the Caddy
# upstream, stop the old) is the real fix and is left for later.
#
# This step MUST fail the script when it fails. It previously read:
#
#   docker compose ... up -d --wait ... || {
#       echo "⚠️ --wait timed out..."
#       docker compose ... logs ... || true
#   }
#
# A `cmd || { ... }` list is exempt from `set -e`, and the block ended
# with `|| true`, so the whole compound returned 0. Every failure —
# "Bind for 127.0.0.1:8080 failed: port is already allocated", a missing
# env_file, a crash on boot — printed a ⚠️ warning and then reported a
# SUCCESSFUL deploy while the previous container kept serving. That
# happened three times in one afternoon and made a green workflow
# worthless as a signal. Diagnosis still runs; only the exit code changed.
echo "→ Starting new container (waiting for healthcheck)..."
if ! docker compose -f deploy/docker-compose.prod.yml up -d --wait "${PROJECT}" 2>&1; then
    echo "✗ Container failed to start. Diagnosing..."
    # These are diagnostic reads: they must not mask the failure above, so
    # their own failures are intentionally ignored.
    docker compose -f "${COMPOSE_FILE}" ps "${PROJECT}" || true
    docker compose -f "${COMPOSE_FILE}" logs --tail 40 "${PROJECT}" || true
    echo "✗ Deploy did NOT apply — the previous container is still serving."
    exit 1
fi

# ── 7. Verify it landed ──
# Step 6 already fails the run when compose fails. This is the second
# net: a container that exists and is healthy is not the same as a deploy
# that applied, because the old container can still be the one serving.
#
# Health is a hard requirement. A start time older than this run is a
# warning, not a failure: `docker compose up -d` legitimately does nothing
# when the service is already up to date, and failing there would make the
# check cry wolf on a no-op deploy. It is reported loudly because it means
# the deploy did not reach a new container.
echo "→ Verifying the running container..."
ACTUAL_HEALTH="$(docker inspect "${PROJECT}" --format '{{.State.Health.Status}}' 2>/dev/null || echo unknown)"
ACTUAL_STARTED="$(docker inspect "${PROJECT}" --format '{{.State.StartedAt}}' 2>/dev/null || echo unknown)"
if [ "${ACTUAL_HEALTH}" != "healthy" ]; then
    echo "✗ Container ${PROJECT} reports health=${ACTUAL_HEALTH}, expected healthy"
    docker compose -f "${COMPOSE_FILE}" logs --tail 40 "${PROJECT}" || true
    exit 1
fi
STARTED_EPOCH="$(date -u -d "${ACTUAL_STARTED}" +%s 2>/dev/null || echo 0)"
if [ "${STARTED_EPOCH}" -gt 0 ] && [ "${STARTED_EPOCH}" -lt "${ROLL_START_EPOCH}" ]; then
    echo "⚠️  ${PROJECT} is healthy but was NOT recreated by this run"
    echo "    (started ${ACTUAL_STARTED}, this deploy began after that)."
    echo "    compose considered it up to date, so the new binary may not be live."
    echo "    Confirm with: docker inspect ${PROJECT} --format '{{.Image}}'"
else
    echo "✓ ${PROJECT} healthy, recreated by this run (started ${ACTUAL_STARTED})"
fi

# ── 7b. Prove the ARTIFACT, not just the container ──
# Healthy + recreated does not prove WHICH build is serving: a failed pull
# leaves the previous container healthy and the deploy looking green. The build
# stamp is the only observable that separates the two, so this is a hard
# requirement. EXPECTED_COMMIT comes from the deploy workflow (the commit the
# image was built from); a manual run falls back to the clone's HEAD.
echo "→ Asserting the serving build..."
"${REPO_DIR}/scripts/assert-deploy-build.sh" \
    "${EXPECTED_COMMIT:-${BUILD_COMMIT}}" "http://127.0.0.1:8080/health"

# ── 8. Report status ──

echo "→ Service status:"
docker compose -f "${COMPOSE_FILE}" ps "${PROJECT}" || true
echo "→ Recent logs:"
docker compose -f "${COMPOSE_FILE}" logs --tail 20 "${PROJECT}" || true
