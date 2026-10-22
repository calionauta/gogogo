#!/bin/bash
# Diff coverage — advisory, always exits 0.
#
# Answers one question the global `make coverage` cannot: of the lines THIS
# change adds, how many does no test execute? A patch that ships 200 added
# lines at 90% repo coverage can still be 0% on the lines it added, and a
# global % gate would stay green while the new behavior rides untested.
#
# What it does: diffs the working tree against HEAD (`BASE` when set, for
# branch review: worktree vs base, so uncommitted lines are included), runs
# the changed packages' tests with a coverage profile (no -race: this is an
# approximation for review, not the race gate), and lists added lines inside
# uncovered blocks.
#
# Usage:
#   bash bin/check-diff-cover.sh              # working tree vs HEAD
#   BASE=origin/master bash bin/check-diff-cover.sh   # branch vs base
#   make cover-diff
#
# Cost note: coverage runs the changed packages' tests once more (without
# -race, ~seconds for a slice). That is why this is on-demand (`make
# cover-diff`) instead of a hook or CI step: the race suite in ci-local is
# the gate; this is the review aid. Untracked files are not in any diff —
# stage them first (`git add -N` is enough to make them visible).
set -uo pipefail

cd "$(git rev-parse --show-toplevel)" || { echo "  ⚠️  not a git worktree — diff-cover skipped"; exit 0; }

base=${BASE:-}
if [ -n "$base" ] && git rev-parse --verify -q "$base" >/dev/null; then
  # Worktree vs base (not base...HEAD): the review aid must see uncommitted
  # lines too, and a triple-dot range would silently exclude them.
  diff_range="$base"
else
  diff_range="HEAD"
fi

go_files=$(git diff -U0 "$diff_range" -- '*.go' | grep -E '^\+\+\+ b/' | sed 's|^\+\+\+ b/||' || true)
if [ -z "$go_files" ]; then
  untracked=$(git status --porcelain -- '*.go' | grep -E '^\?\?' || true)
  if [ -n "$untracked" ]; then
    echo "→ diff-cover: no tracked Go diff (untracked files are not in any diff — stage them with git add -N first)"
    echo "$untracked" | head -5 | sed 's/^/    /'
  else
    echo "→ diff-cover: no Go lines added in $diff_range — nothing to check"
  fi
  exit 0
fi

# Packages to profile: the changed ones — including branch commits when BASE
# is set (forwarded, so the package set and the line set agree). Shared-file
# changes fall back to all packages, same as ci-local-fast — but cap the
# cost: coverage without -race is an approximation, and profiling the whole
# repo on a shared box is exactly what check-host-capacity exists to refuse.
pkgs=$(BASE="$base" bash scripts/changed-packages.sh 2>/dev/null || true)
if [ -z "$pkgs" ]; then
  echo "→ diff-cover: changed-packages resolved to nothing — nothing to check"
  exit 0
fi
n_pkgs=$(echo "$pkgs" | wc -l | tr -d ' ')
if [ "$n_pkgs" -gt 8 ]; then
  echo "→ diff-cover: $n_pkgs packages changed — profiling all of them would cost a full run."
  echo "  Scope it: BASE=<base> bash bin/check-diff-cover.sh, or run make coverage."
  exit 0
fi

tmpdir="${TMPDIR:-/tmp}"
prof=$(mktemp "$tmpdir/diffcover.XXXXXX")
difffile=$(mktemp "$tmpdir/diffcover-diff.XXXXXX")
trap 'rm -f "$prof" "$difffile"' EXIT
# -p 1: one coverage.out needs every package in a single invocation
# (same reason make coverage serializes). No -race: approximation for review.
if ! go test -count=1 -p 1 -coverprofile="$prof" -covermode=atomic $pkgs >/dev/null 2>&1; then
  echo "→ diff-cover: coverage run failed (tests red — fix those first; diff-cover is a review aid, not the gate)"
  exit 0
fi
git diff -U0 "$diff_range" -- '*.go' > "$difffile"

python3 - "$prof" "$difffile" <<'EOF'
import re, sys

prof_path = sys.argv[1]
diff_path = sys.argv[2]
blocks = {}  # cover path -> list of (start, end, count)
with open(prof_path) as fh:
    for i, line in enumerate(fh):
        if i == 0:
            continue  # mode: atomic
        m = re.match(r'^(.+):(\d+)\.\d+,(\d+)\.\d+ \d+ (\d+)$', line.strip())
        if not m:
            continue
        path, start, end, count = m.group(1), int(m.group(2)), int(m.group(3)), int(m.group(4))
        blocks.setdefault(path, []).append((start, end, count))

added = {}  # repo path -> set of added line numbers
cur, line_no = None, 0
with open(diff_path) as _fh:
    diff = _fh.read().splitlines()
for dl in diff:
    if dl.startswith('+++ b/'):
        cur = dl[len('+++ b/'):]
        added.setdefault(cur, set())
    elif dl.startswith('@@'):
        m = re.search(r'\+(\d+)(?:,(\d+))?', dl)
        if not m:
            continue
        line_no = int(m.group(1))
    elif cur is not None and dl.startswith('+') and not dl.startswith('+++'):
        added[cur].add(line_no)
        line_no += 1
    elif cur is not None and dl.startswith('-') and not dl.startswith('---'):
        pass
    elif cur is not None:
        line_no += 1

uncovered, total, test_lines = [], 0, 0
for path, lines in added.items():
    if path.endswith('_test.go'):
        # Test files never appear in a coverprofile (coverage measures
        # production code; the test IS the coverage). Their value is
        # checked by the other half: go run ./cmd/check-test-value.
        test_lines += len(lines)
        continue
    # cover keys are module-qualified; match by suffix
    key = next((k for k in blocks if k == path or k.endswith('/' + path)), None)
    cov = blocks.get(key, []) if key else []
    for ln in sorted(lines):
        total += 1
        hit = next(((s, e, c) for s, e, c in cov if s <= ln <= e), None)
        if hit is None or hit[2] == 0:
            uncovered.append((path, ln))

print(f"→ diff-cover: {total} added production lines profiled, {len(uncovered)} uncovered", end='')
if test_lines:
    print(f" (+{test_lines} added test lines — see check-test-value, not coverage)", end='')
print()
for path, ln in uncovered[:20]:
    print(f"    {path}:{ln} — added line never executed")
if len(uncovered) > 20:
    print(f"    … and {len(uncovered) - 20} more")
if uncovered:
    print("  New behavior with no executing test is where TestHandlersTodoRecords-")
    print("  style vacuous predicates hide: add the wire-symptom assert (or run")
    print("  go run ./cmd/check-test-value for the executable half of the check).")
else:
    print("  ✓ every added line is executed by the changed packages' tests")
print()
print("→ test-value on the changed packages (asserts, not lines):")
EOF
# The executable half: test files never appear in a coverprofile, so their
# value is checked here instead — every changed Test must be able to fail.
# AST-only (~1s), advisory: warnings print, only vacuous fails.
# shellcheck disable=SC2086
go run ./cmd/check-test-value $pkgs || true
# Advisory by design: this script reports, the pre-commit test-value job
# (blocking on vacuous) enforces. A review aid must not fail a gate.
exit 0
