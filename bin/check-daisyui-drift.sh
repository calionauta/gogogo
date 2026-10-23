#!/bin/bash
# check-daisyui-drift.sh — is the vendored daisyui copy still honest?
#
# Why this exists: the DaisyUI component vocabulary is NOT written in this
# repo. It lives upstream at https://daisyui.com/SKILL.md and is vendored into
# skills/daisyui/ so the UI delegation resolves at all — a skill's references
# are only reachable as relative paths to a locally installed skill, so a
# URL-only link could never be read reliably by every host. A vendored copy
# with no staleness check silently becomes a fork: upstream adds components,
# this repo keeps enforcing the old vocabulary, and the wrapper skill still
# claims to delegate to "the" library.
#
# The copy is vendored rather than URL-linked because a host that has only a
# URL must web-fetch the page, which is not the file. So: local copy for
# reachability, this check for currency.
#
# Also verifies the vendored file is byte-identical to the pinned hash, so a
# local edit to the vendored copy is caught instead of quietly diverging from
# upstream while the pinned SHA says otherwise.
#
# Exit codes:
#   0  in sync (or check could not run — see below)
#   1  upstream moved, or the vendored copy diverges from its pinned hash
#
# Usage:
#   bin/check-daisyui-drift.sh            # warn loudly, exit 0  (pre-commit)
#   bin/check-daisyui-drift.sh --strict   # exit 1 on drift     (scheduled CI)
#
# A missing network is NOT drift: this check is advisory infrastructure, and
# failing a commit because daisyui.com was unreachable is exactly the kind of
# false alarm that trains people to bypass hooks. It prints what it could not
# verify and exits 0 unless --strict was asked for.

set -uo pipefail

SKILL_DIR="skills/daisyui"
SHA_FILE="$SKILL_DIR/UPSTREAM_SHA"
UPSTREAM_URL="https://daisyui.com/SKILL.md"

STRICT=0
[ "${1:-}" = "--strict" ] && STRICT=1

# fail_or_warn returns the exit status this run should end with once a finding
# has been recorded: 1 under --strict, 0 for the advisory pre-commit call.
fail_or_warn() {
  [ "$STRICT" -eq 1 ] && return 1
  return 0
}

[ -f "$SHA_FILE" ] || { echo "  ❌ $SHA_FILE missing"; fail_or_warn; exit $?; }
PINNED=$(tr -d '[:space:]' < "$SHA_FILE")
[ -n "$PINNED" ] || { echo "  ❌ $SHA_FILE is empty"; fail_or_warn; exit $?; }

# --- 1. Is the vendored copy unmodified relative to its pinned hash? -----------
# A local edit means the pinned hash is a lie: the wrapper claims to delegate
# to upstream content while the tree says otherwise.
DRIFT=0
if [ -f "$SKILL_DIR/SKILL.md" ]; then
  if ! command -v sha256sum >/dev/null 2>&1; then
    echo "  · sha256sum not available — skipping content check"
  else
    ACTUAL=$(sha256sum "$SKILL_DIR/SKILL.md" | awk '{print $1}')
    if [ "$ACTUAL" != "$PINNED" ]; then
      echo "  ❌ $SKILL_DIR/SKILL.md differs from pinned ${PINNED:0:12} (actual ${ACTUAL:0:12}) — upstream content was edited locally"
      DRIFT=1
    else
      echo "  ✓ vendored copy matches ${PINNED:0:12}"
    fi
  fi
else
  echo "  ❌ $SKILL_DIR/SKILL.md missing"
  DRIFT=1
fi

# --- 2. Has upstream moved? ----------------------------------------------------
# Fetch the file and hash it. Offline is not drift: failing a commit because
# the site was unreachable trains people to bypass hooks.
if ! command -v curl >/dev/null 2>&1; then
  echo "  · curl not available — cannot check upstream"
  [ "$DRIFT" -ne 0 ] && { fail_or_warn; exit $?; }
  exit 0
fi

TMP=$(mktemp)
trap 'rm -f "$TMP"' EXIT
if ! curl -s --fail --max-time 20 -o "$TMP" "$UPSTREAM_URL" 2>/dev/null || [ ! -s "$TMP" ]; then
  echo "  · could not reach $UPSTREAM_URL (offline?) — treating as not-drift"
  [ "$DRIFT" -ne 0 ] && { fail_or_warn; exit $?; }
  exit 0
fi

REMOTE=""
if command -v sha256sum >/dev/null 2>&1; then
  REMOTE=$(sha256sum "$TMP" | awk '{print $1}')
fi
if [ -z "$REMOTE" ]; then
  echo "  · could not hash upstream copy — skipping upstream comparison"
  [ "$DRIFT" -ne 0 ] && { fail_or_warn; exit $?; }
  exit 0
fi

if [ "$REMOTE" = "$PINNED" ]; then
  echo "  ✓ up to date with $UPSTREAM_URL (${PINNED:0:12})"
else
  echo "  ⚠️  upstream has moved: ${PINNED:0:12} → ${REMOTE:0:12}"
  echo "     Refresh the vendored copy with the sanctioned, self-contained path:"
  echo "       scripts/refresh-daisyui.sh             # update to current upstream"
  echo "       scripts/refresh-daisyui.sh --dry-run   # show what would change"
  DRIFT=1
fi

[ "$DRIFT" -eq 0 ] && exit 0
fail_or_warn
exit $?
