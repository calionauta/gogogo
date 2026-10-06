#!/bin/bash
# check-stelow-drift.sh — has upstream stelow-workflow-coding-standards moved
# past the copy vendored in skills/stelow-workflow-coding-standards/?
#
# Why this exists: the universal coding principles (KISS, DRY, LoB/SoC, YAGNI,
# file/function sizes) are NOT written in this repo. They live upstream in
# calionauta/stelow and are vendored here so the delegation resolves. A vendored
# copy with no staleness check silently becomes a fork: upstream fixes land and
# this repo keeps enforcing the old rules while its SKILL.md still claims to
# delegate to "the" standards.
#
# The copy is vendored rather than URL-linked because a skill's `references/`
# are only reachable as RELATIVE PATHS to a locally installed skill. A host that
# has only a URL cannot resolve `references/file-function-sizes.md` — it would
# have to web-fetch the GitHub HTML view, which is not the file. So: local copy
# for reachability, this check for currency.
#
# Also verifies the vendored files are byte-identical to the pinned commit, so a
# local edit to the vendored copy is caught instead of quietly diverging from
# upstream while the pinned SHA says otherwise.
#
# Exit codes:
#   0  in sync (or check could not run — see below)
#   1  upstream moved, or the vendored copy diverges from its pinned SHA
#
# Usage:
#   bin/check-stelow-drift.sh            # warn loudly, exit 0  (pre-commit)
#   bin/check-stelow-drift.sh --strict   # exit 1 on drift     (scheduled CI)
#
# A missing `git`/network is NOT drift: this check is advisory infrastructure,
# and failing a commit because GitHub was unreachable is exactly the kind of
# false alarm that trains people to bypass hooks. It prints what it could not
# verify and exits 0 unless --strict was asked for.

set -uo pipefail

SKILL_DIR="skills/stelow-workflow-coding-standards"
SHA_FILE="$SKILL_DIR/UPSTREAM_SHA"
UPSTREAM="https://github.com/calionauta/stelow.git"
UPSTREAM_PATH="skills/stelow-workflow-coding-standards"
BRANCH="main"

STRICT=0
[ "${1:-}" = "--strict" ] && STRICT=1

# DRIFT accumulates every reason this check would fail. The exit code is
# derived from it once, at the end, so a finding can never be printed without
# also being able to fail (an earlier version printed the content mismatch and
# then exited 0, because the upstream branch's `exit 0` won).
DRIFT=0

# fail_or_warn returns the exit status this run should end with once a finding
# has been recorded: 1 under --strict, 0 for the advisory pre-commit call.
fail_or_warn() {
  [ "$STRICT" -eq 1 ] && return 1
  return 0
}

[ -f "$SHA_FILE" ] || { echo "  ❌ $SHA_FILE missing"; fail_or_warn; exit $?; }
PINNED=$(tr -d '[:space:]' < "$SHA_FILE")
[ -n "$PINNED" ] || { echo "  ❌ $SHA_FILE is empty"; fail_or_warn; exit $?; }

# --- 1. Is the vendored copy unmodified relative to its pinned commit? --------
# A local edit means the pinned SHA is a lie: SKILL.md claims to be upstream's
# content at that commit while the tree says otherwise. Only attempted when the
# upstream clone is already on disk — we never clone inside a commit hook.
LOCAL_CLONE="${STELOW_CLONE:-$HOME/repos/stelow}"
if [ -d "$LOCAL_CLONE/.git" ]; then
  if git -C "$LOCAL_CLONE" cat-file -e "$PINNED^{commit}" 2>/dev/null; then
    DRIFT_LOCAL=0
    while IFS= read -r f; do
      rel="${f#"$SKILL_DIR"/}"
      if ! git -C "$LOCAL_CLONE" show "$PINNED:$UPSTREAM_PATH/$rel" 2>/dev/null | diff -q - "$f" >/dev/null 2>&1; then
        echo "  ❌ $f differs from pinned ${PINNED:0:12} — upstream content was edited locally"
        DRIFT_LOCAL=1
        DRIFT=1
      fi
    done < <(find "$SKILL_DIR" -type f ! -name UPSTREAM_SHA | sort)
    [ "$DRIFT_LOCAL" -eq 0 ] && echo "  ✓ vendored copy matches ${PINNED:0:12}"
  else
    echo "  · pinned commit ${PINNED:0:12} not in $LOCAL_CLONE — skipping content check"
  fi
else
  echo "  · no stelow clone at $LOCAL_CLONE — skipping content check"
fi

# --- 2. Has upstream main moved? ---------------------------------------------
if ! command -v git >/dev/null 2>&1; then
  echo "  · git not available — cannot check upstream"
  fail_or_warn; exit $?
fi

REMOTE=$(timeout 20 git ls-remote "$UPSTREAM" "refs/heads/$BRANCH" 2>/dev/null | awk '{print $1}')
if [ -z "$REMOTE" ]; then
  # Offline is not drift. Failing a commit because GitHub was unreachable is
  # exactly the false alarm that trains people to bypass hooks.
  echo "  · could not reach $UPSTREAM (offline?) — treating as not-drift"
  fail_or_warn; exit $?
fi

if [ "$REMOTE" = "$PINNED" ]; then
  echo "  ✓ up to date with upstream $BRANCH (${PINNED:0:12})"
else
  echo "  ⚠️  upstream $BRANCH has moved: ${PINNED:0:12} → ${REMOTE:0:12}"
  echo "     Refresh the vendored copy with the sanctioned, self-contained path:"
  echo "       scripts/refresh-stelow.sh             # update to upstream $BRANCH"
  echo "       scripts/refresh-stelow.sh --ref <sha> # pin an explicit commit"
  echo "       scripts/refresh-stelow.sh --dry-run    # show what would change"
  DRIFT=1
fi

[ "$DRIFT" -eq 0 ] && exit 0
fail_or_warn
exit $?
