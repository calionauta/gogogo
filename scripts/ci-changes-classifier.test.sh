#!/usr/bin/env bash
# Contract test for scripts/ci-changes-classifier.sh.
#
# The classifier decides whether to skip every heavy gate, so its own test pins
# the three ways it can be wrong. Each case below names the failure it prevents;
# the "must be false" list is the important one, because a false "true" is a
# green run that skipped the gates.
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
classify="${here}/ci-changes-classifier.sh"

fails=0
check() { # want, description, paths...
  local want="$1" desc="$2"
  shift 2
  local got
  got="$(printf '%s\n' "$@" | bash "$classify")"
  if [ "$got" != "$want" ]; then
    printf 'FAIL  %s\n        want %s, got %s (paths: %s)\n' "$desc" "$want" "$got" "$*"
    fails=$((fails + 1))
  else
    printf 'ok    %s\n' "$desc"
  fi
}

echo "--- inert changesets must be recognized as docs-only ---"
check true "plain docs page"                 "docs/rbac.md"
check true "generated site output"           "site/index.html"
check true "README at the repo root"         "README.md"
check true "AGENTS.md at the repo root"      "AGENTS.md"
check true "license text"                    "LICENSE"
check true "gitignore"                       ".gitignore"
check true "stof reference prose"            "references/nats/jetstream-patterns.md"
check true "several inert files at once"     "docs/a.md" "site/index.html" "README.md"

echo
echo "--- impactful changesets must never be docs-only ---"
check false "Go source"                      "internal/auth/session.go"
check false "CSS input (changes the bundle)" "src/css/input.css"
check false "a migration"                    "internal/db/migrations/001.sql"
check false ".templ source"                  "features/todo/components/page.templ"
check false "CI workflow itself"             ".github/workflows/ci.yml"
check false "a script a gate runs"           "scripts/lint-safe.sh"
check false "mixed: prose plus one Go file"  "docs/a.md" "internal/server/chi.go"

echo
echo "--- the false-green cases (regression guards) ---"
# A .md file inside a CODE directory is prose, not code: treating it as
# impactful would only cost a run, and treating it as inert is correct. Pinned
# so the regex is never "fixed" into something broader by accident.
check true  "prose inside a code directory"  "internal/server/README.md"
# A top-level directory nobody allowlisted is impactful until it is allowlisted:
# the classifier must fail closed on the unknown.
check false "unknown top-level directory"    "assets/logo.png"
check false "unknown file at the root"       "Makefile"
# Empty changeset: nothing to run, but only when the diff really is empty.
# (The workflow handles an unreadable base by not calling the classifier at
# all, which keeps the fail-open decision in one place.)
check true  "empty changeset is inert"       ""

echo
if [ "$fails" -ne 0 ]; then
  printf '%d case(s) failed\n' "$fails"
  exit 1
fi
echo "classifier contract: all cases pass"
