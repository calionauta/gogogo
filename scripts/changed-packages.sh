#!/usr/bin/env bash
# Lists the Go package directories affected by the current change, so a fast
# gate can run only what moved instead of the whole repo.
#
# Input: staged + unstaged + untracked changes vs HEAD, plus (optionally) the
# commits on this branch when BASE is set. Falls back to the full package list
# when it cannot narrow safely (a shared file changed, or the diff is unreadable).
#
# Usage:
#   bash scripts/changed-packages.sh                          # working-tree changes
#   BASE=origin/master bash scripts/changed-packages.sh       # + branch commits
#   bash scripts/changed-packages.sh --with-dependents        # + reverse closure
#
# Output: one package directory per line (the same shape web-packages.sh emits,
# which works for `go test` and `golangci-lint`).
#
# The two modes answer two different questions from one diff, and the difference
# is a real one, not a preference:
#
#   (default)         "which packages did this diff touch?" — the scope a LINTER
#                     needs. Linting a caller because a callee moved only burns
#                     time; the caller's own code did not change.
#   --with-dependents "which packages must be TESTED?" — the scope a test run
#                     needs, and it is the closure of everything that can break:
#                     a touched package is compiled into, and exercised by, every
#                     package that imports it (transitively), including through
#                     test imports — an integration test that imports the changed
#                     package is exactly the test that catches the regression.
#                     A touched .templ seeds its directory too: the generated
#                     _templ.go is what enters the build, and `make templ` may
#                     not have run yet.
#
# Both modes fail open to the full list. Running too much costs time; running
# too little costs a broken merge.

set -euo pipefail

project_dir=$(pwd)

usage() {
  cat <<'EOF'
usage: bash scripts/changed-packages.sh [--with-dependents]

  (no flag)          package directories touched by the diff (lint scope)
  --with-dependents  the above, plus every package that transitively imports one
                     of them (imports, test imports, external test imports) and
                     every package directory with a changed .templ (test scope)

  BASE=<ref>         also include the commits from <ref> to HEAD
EOF
}

with_dependents=0
for arg in "$@"; do
  case "$arg" in
    --with-dependents) with_dependents=1 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "changed-packages: unknown argument: $arg" >&2; usage >&2; exit 2 ;;
  esac
done

all_packages() { go list -f '{{.Dir}}' ./... | grep -vE "/cmd/(desktop|gui)$|^${project_dir}$"; }

# Files that, when touched, invalidate every package: build/config plumbing.
shared_pattern='^(go\.mod|go\.sum|Makefile|config/|db/|internal/capabilities/)'

# Prints the changed paths. Returns non-zero when a source could not be read, so
# the caller can tell "nothing changed" apart from "the tree is not readable" —
# they look identical from the output alone.
base=${BASE:-}
collect_changes() {
  local rc=0
  git diff --name-only HEAD 2>/dev/null || rc=1
  git diff --name-only --cached 2>/dev/null || rc=1
  git ls-files --others --exclude-standard 2>/dev/null || rc=1
  if [ -n "$base" ] && git rev-parse --verify -q "$base" >/dev/null 2>&1; then
    git diff --name-only "$base"...HEAD 2>/dev/null || rc=1
  fi
  return "$rc"
}

diff_readable=1
raw=$(collect_changes) || diff_readable=0
files=$(printf '%s\n' "$raw" | grep -v '^$' | sort -u || true)

# Reverse closure over the module's real import graph. The graph comes from one
# `go list` call (no `go mod graph`, no parsing go.mod): every package, with its
# production, in-package-test and external-test imports.
# (`read -d ''` takes the heredoc verbatim; it never returns 0 at EOF, hence || true.)
read -r -d '' dependents_py <<'PY' || true
import sys

project_dir = sys.argv[1]
seeds = set(sys.argv[2:])


def split_imports(field):
    return [p for p in field.split(",") if p]


rows = {}
for line in sys.stdin:
    parts = line.rstrip("\n").split("|")
    if len(parts) != 5:
        continue
    pkg_dir, import_path, imports, test_imports, xtest_imports = parts
    rows[pkg_dir] = (import_path, split_imports(imports) + split_imports(test_imports) + split_imports(xtest_imports))

# Same package set web-packages.sh emits: cmd/desktop and cmd/gui are validated by
# their own workflow, and the module root is not a testable package.
def in_scope(d):
    return d != project_dir and not d.endswith("/cmd/desktop") and not d.endswith("/cmd/gui")


dir_by_import = {import_path: d for d, (import_path, _) in rows.items() if in_scope(d)}

# A seed whose package is not in the graph — a deletion that took the whole
# package — has no place in the dependency order: what it breaks is exactly what
# the graph would have said, so the safe answer is every package. (Fail open;
# selecting a subset here is how a deleted package reaches master untested.)
if any(seed not in rows for seed in seeds):
    print("\n".join(sorted(dir_by_import.values())))
    sys.exit(0)

# Invert the edges, keyed by import path rather than by directory: an import names
# a path, and a caller of a package that is already broken must still be selected.
dependents = {}
for d, (import_path, imported) in rows.items():
    if not in_scope(d):
        continue
    for target in imported:
        dependents.setdefault(target, set()).add(import_path)

# Seeds are package import paths; every one of them exists, per the guard above.
seen = {rows[seed][0] for seed in seeds}
queue = list(seen)
while queue:
    for dependent in dependents.get(queue.pop(), ()):
        if dependent not in seen:
            seen.add(dependent)
            queue.append(dependent)

found = sorted(dir_by_import[p] for p in seen if p in dir_by_import)
if found:
    print("\n".join(found))
PY

expand_dependents() {
  if ! command -v python3 >/dev/null 2>&1; then
    echo "changed-packages: python3 not found, falling back to all packages" >&2
    all_packages
    return 0
  fi
  # Capture the graph first: a half-read listing must never narrow the selection.
  local graph
  if ! graph=$(go list -e -f '{{.Dir}}|{{.ImportPath}}|{{join .Imports ","}}|{{join .TestImports ","}}|{{join .XTestImports ","}}' ./...); then
    echo "changed-packages: cannot list the package graph, falling back to all packages" >&2
    all_packages 2>/dev/null || true
    return 0
  fi
  printf '%s\n' "$graph" | python3 -c "$dependents_py" "$project_dir" "$@"
}

# An unreadable tree must widen, never narrow. (The default mode keeps the old
# best-effort reading of whatever sources did answer: lint is cheap to widen by
# hand and callers depend on that behaviour.)
if [ "$with_dependents" -eq 1 ] && [ "$diff_readable" -eq 0 ]; then
  echo "changed-packages: cannot read the working tree, falling back to all packages" >&2
  all_packages
  exit 0
fi

if [ -z "$files" ]; then
  echo "changed-packages: no changes detected" >&2
  exit 0
fi

if echo "$files" | grep -qE "$shared_pattern"; then
  echo "changed-packages: shared file changed, falling back to all packages" >&2
  all_packages
  exit 0
fi

# Seed the package of every changed .go file. In test scope a changed .templ
# also seeds its directory: the .templ is not a Go file, but its generated
# _templ.go is what compiles into the package.
if [ "$with_dependents" -eq 1 ]; then
  seed_files=$(echo "$files" | grep -E '\.(go|templ)$' || true)
else
  seed_files=$(echo "$files" | grep -E '\.go$' || true)
fi
if [ -z "$seed_files" ]; then
  exit 0
fi

rel_dirs=$(
  echo "$seed_files" | while read -r f; do
    d=$(dirname "$f")
    # Test scope: a deleted file still moved its package, as long as the package
    # (its directory) is still there.
    if [ -f "$f" ] || { [ "$with_dependents" -eq 1 ] && [ -d "$d" ]; }; then
      echo "$d"
    fi
  done | sort -u
)

if [ "$with_dependents" -eq 1 ]; then
  seeds=()
  for d in $rel_dirs; do
    abs=$project_dir/$d
    [ "$d" = "." ] && abs=$project_dir
    seeds+=("$abs")
  done
  if [ "${#seeds[@]}" -eq 0 ]; then
    exit 0
  fi
  expand_dependents "${seeds[@]}"
  exit 0
fi

# Keep only dirs that are real packages in the module.
all=$(all_packages)
for d in $rel_dirs; do
  abs=$project_dir/$d
  [ "$d" = "." ] && abs=$project_dir
  if echo "$all" | grep -qxF "$abs"; then
    echo "$abs"
  fi
done
