#!/usr/bin/env bash
# Contract test for bin/check-operator-look.sh.
#
# The gate exists because "ask the operator" lived only in prose, so an agent
# could run `make signoff` without ever asking. A gate that cannot fail would
# not have fixed that, so this test drives the three real states against a
# throwaway git repo: no look recorded, a FAIL recorded, and a PASS recorded for
# a DIFFERENT commit (the stale case that matters after an edit).
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
check="${here}/check-operator-look.sh"
root="${here}/.."
look="${root}/scripts/operator-look.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fails=0
ok() { printf 'ok    %s\n' "$1"; }
bad() { printf 'FAIL  %s\n        %s\n' "$1" "$2"; fails=$((fails + 1)); }

# new_repo builds a repo whose HEAD is a real commit, with an origin/master so
# the diff base is a merge-base rather than the "no origin" fallback.
new_repo() {
	local dir="$1"
	mkdir -p "$dir"
	git -C "$dir" init -q -b master
	git -C "$dir" config user.email t@example.com
	git -C "$dir" config user.name test
	printf 'package main\n' >"$dir/main.go"
	git -C "$dir" add .
	git -C "$dir" commit -qm base
	git -C "$dir" update-ref refs/remotes/origin/master HEAD
	mkdir -p "$dir/bin" "$dir/scripts"
	cp "$check" "$dir/bin/"
	cp "$look" "$dir/scripts/"
}

run_gate() {
	local dir="$1"
	(cd "$dir" && bash bin/check-operator-look.sh >/dev/null 2>&1)
}

# 1. A Go-only diff is machine-decidable: pass with no stamp.
dir="$tmp/go-only"
new_repo "$dir"
printf 'package main\n\nfunc main() {}\n' >"$dir/main.go"
git -C "$dir" commit -qam "go only"
if run_gate "$dir"; then ok "a Go-only diff needs no operator look"; else bad "a Go-only diff needs no operator look" "gate demanded a look"; fi

# 2. A .templ change with no stamp must FAIL.
dir="$tmp/templ"
new_repo "$dir"
mkdir -p "$dir/features/x/components"
printf 'package components\n' >"$dir/features/x/components/page.templ"
git -C "$dir" add .
git -C "$dir" commit -qm "page"
if run_gate "$dir"; then bad "a page change without a look is refused" "gate passed with no stamp"; else ok "a page change without a look is refused"; fi

# 3. A FAIL recorded for HEAD must FAIL.
(cd "$dir" && bash scripts/operator-look.sh fail "label is wrong" >/dev/null)
if run_gate "$dir"; then bad "a recorded FAIL is refused" "gate passed on FAIL"; else ok "a recorded FAIL is refused"; fi

# 4. A PASS recorded for a different commit must FAIL (the edit-after-look case).
(cd "$dir" && bash scripts/operator-look.sh pass "looks right" >/dev/null)
python3 - "$dir/.gogogo/verify-local.json" <<'PY'
import json, sys
p = sys.argv[1]
d = json.load(open(p))
d["commit"] = "0" * 40
json.dump(d, open(p, "w"))
PY
if run_gate "$dir"; then bad "a stale PASS is refused" "gate passed on an old commit"; else ok "a stale PASS is refused"; fi

# 5. A PASS for HEAD opens the gate; an edit then closes it again.
(cd "$dir" && bash scripts/operator-look.sh pass "looks right" >/dev/null)
if run_gate "$dir"; then ok "a PASS for HEAD opens the gate"; else bad "a PASS for HEAD opens the gate" "gate refused a fresh PASS"; fi
printf 'package components\n// edited after the look\n' >"$dir/features/x/components/page.templ"
git -C "$dir" commit -qam "edit after look"
if run_gate "$dir"; then bad "an edit after the look re-closes it" "gate passed a stale PASS"; else ok "an edit after the look re-closes it"; fi

# 6. An UNCOMMITTED edit after the look also re-closes it. This is the case that
# matters while iterating: the commit is unchanged, only the worktree moved.
(cd "$dir" && bash scripts/operator-look.sh pass "looks right" >/dev/null)
printf 'package components\n// uncommitted tweak\n' >"$dir/features/x/components/page.templ"
if run_gate "$dir"; then bad "an uncommitted edit after the look re-closes it" "gate passed a stale worktree"; else ok "an uncommitted edit after the look re-closes it"; fi

if [ "$fails" -ne 0 ]; then
	printf '\n%d failure(s)\n' "$fails"
	exit 1
fi
echo "✅ operator-look contract holds"
