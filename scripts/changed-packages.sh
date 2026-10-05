#!/usr/bin/env bash
# Lists the Go package directories affected by the current change, so a fast
# local gate can test only what moved instead of the whole repo.
#
# Input: staged + unstaged + untracked changes vs HEAD, plus (optionally) the
# commits on this branch when BASE is set. Falls back to the full package list
# when it cannot narrow safely (a shared file changed, or nothing changed).
#
# Usage:
#   bash scripts/changed-packages.sh              # working-tree changes
#   BASE=origin/master bash scripts/changed-packages.sh   # + branch commits
#
# Output: one package directory per line (the same shape web-packages.sh emits,
# which works for `go test` and `golangci-lint`).

set -euo pipefail

project_dir=$(pwd)
all_packages() { go list -f '{{.Dir}}' ./... | grep -vE "/cmd/(desktop|gui)$|^${project_dir}$"; }

# Files that, when touched, invalidate every package: build/config plumbing.
shared_pattern='^(go\.mod|go\.sum|Makefile|config/|db/|internal/capabilities/)'

base=${BASE:-}
files=$(
  {
    git diff --name-only HEAD 2>/dev/null || true
    git diff --name-only --cached 2>/dev/null || true
    git ls-files --others --exclude-standard 2>/dev/null || true
    if [ -n "$base" ] && git rev-parse --verify -q "$base" >/dev/null; then
      git diff --name-only "$base"...HEAD 2>/dev/null || true
    fi
  } | sort -u
)

if [ -z "$files" ]; then
  echo "changed-packages: no changes detected, falling back to all packages" >&2
  all_packages
  exit 0
fi

if echo "$files" | grep -qE "$shared_pattern"; then
  echo "changed-packages: shared file changed, falling back to all packages" >&2
  all_packages
  exit 0
fi

# Map each changed .go file to its package directory, then reduce to the set of
# packages. Non-Go changes (CSS, .templ, docs) do not add a package here — the
# caller decides what else to run.
go_files=$(echo "$files" | grep -E '\.go$' || true)
if [ -z "$go_files" ]; then
  exit 0
fi

dirs=$(
  echo "$go_files" | while read -r f; do
    [ -f "$f" ] || continue
    dirname "$f"
  done | sort -u
)

# Keep only dirs that are real packages in the module.
all=$(all_packages)
for d in $dirs; do
  abs=$project_dir/$d
  [ "$d" = "." ] && abs=$project_dir
  if echo "$all" | grep -qxF "$abs"; then
    echo "$abs"
  fi
done
