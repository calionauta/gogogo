#!/usr/bin/env bash
# check-operator-look — refuse a signoff when the diff needs a human look and
# the operator never gave one.
#
# Why this exists: docs/local-ci.md says where the operator's click belongs
# (after ci-local-fast, before ci-local/signoff/push) and `make verify-local`
# makes the click cheap. But prose is not a gate: an agent that never asks can
# still run `make signoff` and push, and the missing judgment is invisible until
# someone opens the page. This script is the mechanical half of that rule.
#
# What it decides, and only this:
#   1. which files the branch changed (vs `origin/master`), and
#   2. whether any of them is an OPERATOR-JUDGMENT surface — a .templ page, the
#      shared CSS, or the shell's components. A Go-only, test-only or docs-only
#      diff is machine-decidable and passes with no stamp.
#   3. if a judgment surface changed, a PASS stamp for THIS commit must exist.
#
# The stamp is written by `scripts/verify-local.sh` (interactive) or
# `scripts/operator-look.sh pass|fail`, which the AGENT runs only after the
# operator answered. It records the commit SHA, so an edit after the look
# invalidates it instead of silently blessing different code.
#
# Exit 0 = nothing to look at, or a look was recorded. Exit 1 = go ask.
set -uo pipefail
cd "$(dirname "$0")/.."

diff_base() {
	if git rev-parse --verify --quiet origin/master >/dev/null; then
		git merge-base origin/master HEAD
		return
	fi
	# A fresh clone with no origin/master (worktree, fork) compares against the
	# empty tree: every file in the commit is "changed", which over-asks rather
	# than under-asks.
	git rev-parse HEAD~1 2>/dev/null || git hash-object -t tree /dev/null
}

# judgment_surfaces are the files whose acceptance is a human judgment: a
# rendered page, the source of the compiled stylesheet, or the shared chrome.
# Deliberately narrow: a Go file under components/ is logic a test decides, and
# asking a human to look at it trains the click that this gate exists to stop.
judgment_surfaces='\.templ$|^src/css/|^web/resources/static/chrome\.css$'

base=$(diff_base)
changed=$(git diff --name-only "$base" HEAD 2>/dev/null; git diff --name-only HEAD 2>/dev/null; git ls-files --others --exclude-standard 2>/dev/null)
changed=$(printf '%s\n' "$changed" | sort -u | sed '/^$/d')

# worktree_digest fingerprints what the operator actually LOOKED at: the diff
# and the untracked files, not just the commit. Without it an edit after the
# look — the common case while iterating — would keep a stamp that blesses
# different code.
worktree_digest() {
	{
		git diff HEAD
		git ls-files -o --exclude-standard -z -- . ':(exclude).gogogo' | while IFS= read -r -d "" f; do
			printf '== %s\n' "$f"
			git hash-object --no-filters -- "$f"
		done
	} | git hash-object --stdin
}

looked=$(printf '%s\n' "$changed" | grep -E "$judgment_surfaces" || true)

if [ -z "$looked" ]; then
	echo "✅ operator look: not needed (no operator-judgment surface in this diff)"
	exit 0
fi

stamp=".gogogo/verify-local.json"
if [ ! -f "$stamp" ]; then
	echo "❌ operator look: required — this diff changes pages the gates cannot judge:"
	printf '   %s\n' $(printf '%s\n' "$looked" | head -8)
	echo
	echo "   Run: make verify-local        (boots on :8098, prints URL + the routes the diff touched)"
	echo "   Then record the operator's answer:"
	echo "     make operator-look-pass     # the operator said PASS"
	echo "     make operator-look-fail     # the operator said FAIL — fix before signoff"
	exit 1
fi

head=$(git rev-parse HEAD)
tree=$(worktree_digest)
node -e '
const fs = require("fs");
const p = process.argv[1];
let s;
try { s = JSON.parse(fs.readFileSync(p, "utf8")); } catch { console.error("bad stamp: " + p); process.exit(2); }
process.stdout.write([s.commit || "", s.tree || "", s.verdict || "", s.at || ""].join("\t"));
' "$stamp" >/tmp/operator-look.$$ 2>/dev/null
IFS=$'\t' read -r commit stampedTree verdict at < /tmp/operator-look.$$ || true
rm -f /tmp/operator-look.$$

if [ "$verdict" = "fail" ]; then
	echo "❌ operator look: FAIL recorded at ${at:-?} — fix, then look again"
	exit 1
fi
if [ "$commit" != "$head" ] || [ "$stampedTree" != "$tree" ]; then
	echo "❌ operator look: stale — the code changed after the look"
	echo "   recorded ${commit:-?}/${stampedTree:-?}; now $head/$tree"
	echo "   Run make verify-local again for the current tree."
	exit 1
fi

echo "✅ operator look: PASS recorded for $head at ${at:-?}"
