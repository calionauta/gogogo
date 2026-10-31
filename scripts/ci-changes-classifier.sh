#!/usr/bin/env bash
# Decide whether a changeset is docs-only, i.e. whether it can skip every
# heavy gate (checks, race, browser, prod-image).
#
# Reads changed paths from stdin, one per line. Prints "true" or "false".
#
# This is a PURE function over paths on purpose. The decision used to be inline
# YAML in .github/workflows/ci.yml, where no test could reach it — and a wrong
# "true" silently skips lint, the race suite, the browser suites and the
# production-image boot while every job that DOES run reports green. That is a
# green build that proves less than it looks like (the bug class the Boardy CI
# post names: "we hid a server change from Nx on purpose and all six shards
# skipped their tests and reported green").
#
# The policy is an allowlist of INERT trees, not a list of code paths: a new
# top-level directory is treated as impactful until someone adds it here. That
# direction is deliberate — a false "false" costs a CI run, a false "true"
# costs a shipped break.
set -euo pipefail

# Paths that cannot affect the built binary, the embedded CSS, the SQL, or any
# gate's verdict.
#
# docs/ and site/ are prose and the generated site; *.md anywhere is prose by
# extension (README, AGENTS.md, ARCHITECTURE.md, references/*.md, the PR
# template); LICENSE/.gitignore/.stelow are inert. NB: docs/*.md is still
# validated — pages.yml runs site-check on every push, and ci.yml's own `site`
# job is NOT gated by this classifier — so "docs-only" means "the Go/image/
# browser gates have nothing to say about it", not "nothing validates it".
inert_re='^(docs/|site/|\.stelow/)|\.md$|^LICENSE$|^\.gitignore$'

docs_only=true
while IFS= read -r path; do
  [ -z "$path" ] && continue
  if ! printf '%s\n' "$path" | grep -qE "$inert_re"; then
    docs_only=false
    break
  fi
done

printf '%s\n' "$docs_only"
