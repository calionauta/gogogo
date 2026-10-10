#!/bin/bash
# check-secrets — pre-commit secret scan (gitleaks protect on staged changes).
#
# 2026 context: leaked credentials in git remain the top secret vector and
# AI-assisted commits leak at ~2x the baseline (GitGuardian 2026) — a scan
# that only runs in CI is too late (history already written). This runs on
# staged content before the commit exists.
#
# Follows the repo tool convention: auto-install the pinned binary when
# missing, then fail closed (exit 1 on findings). Pinned, not @latest: a
# new release can add detectors that fail previously-green trees.
set -uo pipefail

GITLEAKS_VERSION="v8.30.1"
# NOTE: the module path keeps the old org name (go.mod declares
# github.com/zricethezav/gitleaks/v8); installing via the new org path
# fails with a version-constraints conflict.
if ! command -v gitleaks >/dev/null 2>&1; then
  echo "→ Installing gitleaks $GITLEAKS_VERSION..."
  go install "github.com/zricethezav/gitleaks/v8@$GITLEAKS_VERSION"
fi
echo "→ gitleaks protect (staged changes)..."
gitleaks protect --staged --verbose --redact || { echo "❌ Possible secret in staged changes — see above (redacted)"; exit 1; }
echo "  ✓ no secrets in staged changes"
