#!/bin/bash
# refresh-daisyui.sh — update the vendored daisyui copy to the current upstream
# file, and bump UPSTREAM_SHA.
#
# This is the OTHER half of bin/check-daisyui-drift.sh: that script only DETECTS
# that upstream moved or the vendored copy was edited; this one performs the
# update. They are split because the check runs in a commit hook and must never
# write, while the refresh is a deliberate, reviewed action.
#
# Why self-contained: the upstream is a single HTTP file
# (https://daisyui.com/SKILL.md), not a git repo, so there is no clone and no
# commit SHA to pin. UPSTREAM_SHA holds the sha256 of the vendored bytes, and
# the refresh re-fetches the URL with curl — no API token, no rate-limit
# exposure beyond one file.
#
# Usage:
#   scripts/refresh-daisyui.sh           # update to current upstream
#   scripts/refresh-daisyui.sh --dry-run # show what would change
#
# Exit codes:
#   0  vendored copy updated (or already current)
#   1  could not refresh (network, missing curl, fetch failure)
#
# After a successful run, review with `git diff` and commit. The pinned hash and
# the vendored bytes are checked by bin/check-daisyui-drift.sh.

set -uo pipefail

SKILL_DIR="skills/daisyui"
SHA_FILE="$SKILL_DIR/UPSTREAM_SHA"
UPSTREAM_URL="https://daisyui.com/SKILL.md"

DRY_RUN=0
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=1 ;;
    -h|--help) sed -n '2,27p' "$0"; exit 0 ;;
    *) echo "refresh-daisyui: unknown argument $arg" >&2; exit 1 ;;
  esac
done

command -v curl >/dev/null 2>&1 || { echo "refresh-daisyui: curl is required" >&2; exit 1; }
command -v sha256sum >/dev/null 2>&1 || { echo "refresh-daisyui: sha256sum is required" >&2; exit 1; }

TMP=$(mktemp)
trap 'rm -f "$TMP"' EXIT

if ! curl -s --fail --max-time 25 -o "$TMP" "$UPSTREAM_URL"; then
  echo "refresh-daisyui: fetch failed ($UPSTREAM_URL)" >&2
  exit 1
fi
[ -s "$TMP" ] || { echo "refresh-daisyui: fetched file is empty" >&2; exit 1; }

# A SKILL.md that does not start with frontmatter would break the skill silently.
if [ "$(head -1 "$TMP")" != "---" ]; then
  echo "refresh-daisyui: fetched SKILL.md does not start with '---' — refusing to install" >&2
  exit 1
fi

TARGET=$(sha256sum "$TMP" | awk '{print $1}')
CURRENT=""
[ -f "$SHA_FILE" ] && CURRENT=$(tr -d '[:space:]' < "$SHA_FILE")
echo "→ target sha256: $TARGET"
echo "  current pin:   ${CURRENT:-<none>}"

if [ -f "$SKILL_DIR/SKILL.md" ] && cmp -s "$TMP" "$SKILL_DIR/SKILL.md"; then
  echo "  ✓ already current — nothing to do"
  if [ "$CURRENT" != "$TARGET" ]; then
    echo "  ⚠️  bytes match but $SHA_FILE differs; repairing the pin"
    if [ "$DRY_RUN" -eq 1 ]; then
      echo "  (dry run — nothing written)"
      exit 0
    fi
    printf '%s\n' "$TARGET" > "$SHA_FILE"
    echo "  ✓ pin repaired"
  fi
  exit 0
fi

if [ "$DRY_RUN" -eq 1 ]; then
  echo "  + upstream differs (dry run — nothing written)"
  exit 0
fi

cp "$TMP" "$SKILL_DIR/SKILL.md"
printf '%s\n' "$TARGET" > "$SHA_FILE"
echo "  ✓ vendored copy updated; $SHA_FILE bumped"

# --- verify -------------------------------------------------------------------
echo "→ verifying"
if ! bin/check-daisyui-drift.sh --strict; then
  echo "  ❌ post-refresh verification failed" >&2
  exit 1
fi
echo "  Review with 'git diff $SKILL_DIR', then commit."
