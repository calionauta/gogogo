#!/usr/bin/env bash
# check-surface on the STAGED tree, not the working tree.
#
# The gate answers "does this tree have an export nobody consumes". Run from a
# pre-commit hook against the working directory, it answers that question about
# files that are NOT in the commit: an unrelated change being edited in the same
# directory (an i18n sweep, a half-finished refactor) makes the gate report the
# other change's findings and, worse, makes a clean run prove nothing about what
# is about to be committed.
#
# That is not hypothetical. Commits here were blocked by findings belonging to
# an unrelated branch in the same worktree; the same tree passed in CI (which
# only ever sees the commit) and failed locally. The gate was right both times —
# it was measuring a different tree than the one being committed.
#
# So: write the index into a temporary directory and measure THAT. git's own
# `checkout-index --prefix` does it in one step, without a second worktree (a
# worktree cannot be created while a commit is in progress — the index is
# locked, which is exactly when this runs).
#
# What the temp copy does NOT have is untracked files, which is the point: the
# question is about the commit.
set -euo pipefail

tmp=$(mktemp -d "${TMPDIR:-/tmp}/surface-index.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

# The staged content of every tracked path, at its index mode.
if ! git checkout-index --all --force --prefix="$tmp/" >/dev/null 2>&1; then
	echo "check-surface: could not materialise the index; run 'make check-surface' by hand" >&2
	exit 2
fi

# Files staged as deleted are absent from the index, so they are absent here —
# correct. Files never tracked are absent too, also correct: they are not in the
# commit.
exec go run ./cmd/check-surface -dir "$tmp"
