#!/bin/bash
# refresh-stelow.sh — update the vendored stelow-workflow-coding-standards copy
# to a newer upstream commit, and bump UPSTREAM_SHA.
#
# This is the OTHER half of bin/check-stelow-drift.sh: that script only DETECTS
# that upstream moved; this one performs the update. They are split because the
# check runs in a commit hook and must never write or need the network beyond a
# read-only ls-remote, while the refresh is a deliberate, reviewed action.
#
# Why self-contained: the original refresh procedure was "git clone stelow, cp
# -R, echo SHA", which needs a clone of the whole stelow repo at the right
# commit. Fetching each file from raw.githubusercontent.com/<repo>/<sha>/<path>
# gives byte-identical content with no clone, no GitHub API token, and no
# rate-limit exposure for the content itself.
#
# Discovery of the file list is the one part that needs the API (or a clone),
# because only the upstream tree knows whether a NEW reference was added. If
# neither is reachable this script falls back to the local file list and warns
# loudly — a refresh that cannot see new files is still better than no refresh,
# but it must not pretend to be complete.
#
# Usage:
#   scripts/refresh-stelow.sh                 # update to upstream main
#   scripts/refresh-stelow.sh --ref <sha>     # pin an explicit commit
#   scripts/refresh-stelow.sh --dry-run       # show what would change
#
# Exit codes:
#   0  vendored copy updated (or already current)
#   1  could not refresh (network, missing curl, fetch failure)
#
# After a successful run, review with `git diff` and commit. The pinned SHA and
# the vendored bytes are checked by bin/check-stelow-drift.sh and the weekly
# stelow-drift workflow.

set -uo pipefail

SKILL_DIR="skills/stelow-workflow-coding-standards"
SHA_FILE="$SKILL_DIR/UPSTREAM_SHA"
UPSTREAM_GIT="https://github.com/calionauta/stelow.git"
UPSTREAM_REPO="calionauta/stelow"
UPSTREAM_PATH="skills/stelow-workflow-coding-standards"
RAW_BASE="https://raw.githubusercontent.com/$UPSTREAM_REPO"
BRANCH="main"

REF="$BRANCH"
DRY_RUN=0
while [ $# -gt 0 ]; do
  case "$1" in
    --ref) REF="${2:-}"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) sed -n '2,32p' "$0"; exit 0 ;;
    *) echo "refresh-stelow: unknown argument $1" >&2; exit 1 ;;
  esac
done

command -v curl >/dev/null 2>&1 || { echo "refresh-stelow: curl is required" >&2; exit 1; }

# --- resolve the target commit -------------------------------------------------
# A full SHA is used as-is; anything else is treated as a branch/tag name and
# resolved through git ls-remote, which needs no API token and no rate budget.
if printf '%s' "$REF" | grep -qE '^[0-9a-f]{40}$'; then
  TARGET="$REF"
else
  TARGET=$(timeout 25 git ls-remote "$UPSTREAM_GIT" "refs/heads/$REF" "refs/tags/$REF" 2>/dev/null | awk 'NR==1{print $1}')
  [ -n "$TARGET" ] || { echo "refresh-stelow: could not resolve ref '$REF' upstream" >&2; exit 1; }
fi
echo "→ target commit: $TARGET"

CURRENT=""
[ -f "$SHA_FILE" ] && CURRENT=$(tr -d '[:space:]' < "$SHA_FILE")
if [ "$CURRENT" = "$TARGET" ]; then
  echo "  ✓ already at $TARGET — nothing to do"
  exit 0
fi
echo "  current pin:  ${CURRENT:-<none>}"
echo "  new pin:      $TARGET"

# --- discover the upstream file list -------------------------------------------
# Preferred: the GitHub API tree, which is the only source that reveals a NEWLY
# ADDED reference file. Falls back to a local clone, then to the local list.
UPSTREAM_FILES=""
LOCAL_CLONE="${STELOW_CLONE:-$HOME/repos/stelow}"

api_tree() {
  curl -s --max-time 25 \
    "https://api.github.com/repos/$UPSTREAM_REPO/git/trees/$TARGET?recursive=1" \
  | python3 -c "
import sys, json
try: d = json.load(sys.stdin)
except Exception: raise SystemExit(1)
if 'tree' not in d: raise SystemExit(1)
prefix = '$UPSTREAM_PATH/'
for e in d['tree']:
    p = e.get('path', '')
    if e.get('type') == 'blob' and p.startswith(prefix):
        print(p[len(prefix):])
" 2>/dev/null
}

if UPSTREAM_FILES=$(api_tree) && [ -n "$UPSTREAM_FILES" ]; then
  echo "  file list: GitHub API tree"
elif [ -d "$LOCAL_CLONE/.git" ] && git -C "$LOCAL_CLONE" cat-file -e "$TARGET^{commit}" 2>/dev/null; then
  UPSTREAM_FILES=$(git -C "$LOCAL_CLONE" ls-tree -r --name-only "$TARGET" -- "$UPSTREAM_PATH" \
    | sed "s|^$UPSTREAM_PATH/||")
  echo "  file list: local clone ($LOCAL_CLONE)"
else
  # Cannot see upstream's file list. Refresh the files we already vendor, and
  # say so — a silently incomplete refresh is the failure mode worth avoiding.
  UPSTREAM_FILES=$(find "$SKILL_DIR" -type f ! -name UPSTREAM_SHA -printf '%P\n' | sort)
  echo "  ⚠️  file list: LOCAL ONLY (no API, no clone) — a newly added upstream"
  echo "      reference would NOT be picked up by this run."
fi
[ -n "$UPSTREAM_FILES" ] || { echo "refresh-stelow: upstream file list is empty" >&2; exit 1; }

# --- fetch every file at the target commit -------------------------------------
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

FAILED=0
for rel in $UPSTREAM_FILES; do
  dest="$TMP/$rel"
  mkdir -p "$(dirname "$dest")"
  if ! curl -s --fail --max-time 25 -o "$dest" "$RAW_BASE/$TARGET/$UPSTREAM_PATH/$rel"; then
    echo "  ❌ fetch failed: $rel" >&2
    FAILED=1
    continue
  fi
  [ -s "$dest" ] || { echo "  ❌ empty file: $rel" >&2; FAILED=1; }
done
[ "$FAILED" -eq 0 ] || { echo "refresh-stelow: aborting, $TARGET not fully fetched" >&2; exit 1; }

# A SKILL.md that does not start with frontmatter would break the skill silently.
if [ -f "$TMP/SKILL.md" ] && [ "$(head -1 "$TMP/SKILL.md")" != "---" ]; then
  echo "  ❌ fetched SKILL.md does not start with '---' — refusing to install" >&2
  exit 1
fi
echo "  ✓ fetched $(printf '%s\n' $UPSTREAM_FILES | wc -l | tr -d ' ') file(s)"

# --- report what changes, then install ----------------------------------------
CHANGED=0
NEW=""
for rel in $UPSTREAM_FILES; do
  if [ ! -f "$SKILL_DIR/$rel" ]; then
    NEW="$NEW $rel"
    CHANGED=1
  elif ! cmp -s "$TMP/$rel" "$SKILL_DIR/$rel"; then
    CHANGED=1
  fi
done
# Upstream deleting a reference must delete it here too, or the copy diverges.
GONE=""
if [ -d "$SKILL_DIR" ]; then
  while IFS= read -r local_rel; do
    printf '%s\n' $UPSTREAM_FILES | grep -qxF "$local_rel" || { GONE="$GONE $local_rel"; CHANGED=1; }
  done < <(find "$SKILL_DIR" -type f ! -name UPSTREAM_SHA -printf '%P\n' | sort)
fi

if [ "$CHANGED" -eq 0 ]; then
  echo "  ✓ content already matches $TARGET (only the pin changes)"
fi
[ -n "$NEW" ] && echo "  + added:$NEW"
[ -n "$GONE" ] && echo "  - removed:$GONE"

if [ "$DRY_RUN" -eq 1 ]; then
  echo "  (dry run — nothing written)"
  exit 0
fi

for rel in $UPSTREAM_FILES; do
  mkdir -p "$SKILL_DIR/$(dirname "$rel")"
  cp "$TMP/$rel" "$SKILL_DIR/$rel"
done
for rel in $GONE; do
  rm -f "$SKILL_DIR/$rel"
done
printf '%s\n' "$TARGET" > "$SHA_FILE"
echo "  ✓ vendored copy updated; $SHA_FILE bumped"

# --- verify ---------------------------------------------------------------------
echo "→ verifying"
if ! bin/check-stelow-drift.sh --strict; then
  echo "  ❌ post-refresh verification failed" >&2
  exit 1
fi
echo "  Review with 'git diff $SKILL_DIR', then commit."
