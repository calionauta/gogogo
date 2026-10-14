#!/bin/bash
# check-ci-deps — every CI job provisions its own toolchain and dependencies.
#
# Why: steps used to work through side effects of earlier steps on the same
# runner (the client suites imported `playwright` only because `make css`
# had run `npm ci` minutes earlier). Splitting one serial job into parallel
# jobs broke exactly that: separate jobs get separate runners, so the first
# parallel run failed on `import { chromium } from "playwright"` with no
# node_modules. Local `make ci-local` cannot catch this class — a dev box
# always has node_modules, Go and browsers warm.
#
# The rule is closed and repo-sized (not a generic linter): within one job,
#   * `node <script>`            requires `npm ci` / `npm install` earlier,
#   * `npx <pkg>`                requires `actions/setup-node` earlier,
#   * `go build|test|tool|...`   requires a Go install (`go.dev/dl`, setup-go) earlier.
# Preinstalled runner tools (bash, git, gh, curl, docker) need no gate.
# YAML `#` comments are stripped before matching, so documenting a command
# in a comment cannot trip the check. No network, deterministic, ~instant.
# Bash-3 safe (macOS ships 3.2: no mapfile, no associative arrays).
#
# Same-implementation rule: this script runs pre-commit (globbed on
# .github/workflows/**) and as a step of the checks CI job.
set -uo pipefail

fail=0
tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

for wf in .github/workflows/ci.yml .github/workflows/deploy.yml; do
  [ -f "$wf" ] || continue
  # Annotated lines lineno:job:text for everything under ^jobs:.
  awk '
    /^jobs:/ { injobs=1; next }
    injobs && /^  [A-Za-z0-9_-]+:/ { job=$1; sub(/:$/, "", job); next }
    injobs && job != "" { gsub(/ #.*$/, ""); gsub(/^ *#.*$/, ""); print NR ":" job ":" $0 }
  ' "$wf" > "$tmpdir/ann"
  cut -d: -f2 "$tmpdir/ann" | sort -u > "$tmpdir/jobs"
  while IFS= read -r job; do
    [ -n "$job" ] || continue
    awk -F: -v j="$job" '$2 == j' "$tmpdir/ann" > "$tmpdir/j"
    uses_node=$(grep -E -c '(^|[[:space:];&|])(node (scripts/|-e))' "$tmpdir/j" || true)
    has_npm=$(grep -E -c 'npm (ci|install)' "$tmpdir/j" || true)
    uses_npx=$(grep -E -c '(^|[[:space:];&|])npx ' "$tmpdir/j" || true)
    has_node_setup=$(grep -E -c 'actions/setup-node' "$tmpdir/j" || true)
    uses_go=$(grep -E -c '(^|[[:space:];&|"`$"(])go (build|test|tool|vet|run|install|list|env)' "$tmpdir/j" || true)
    has_go=$(grep -E -c 'go\.dev/dl/go|setup-go' "$tmpdir/j" || true)
    first_use=$(grep -E -n '(^|[[:space:];&|])(node (scripts/|-e)|npx |go (build|test|tool|vet|run|install|list|env))' "$tmpdir/j" | head -n 1 | cut -d: -f1)
    first_prov=$(grep -E -n 'npm (ci|install)|actions/setup-node|go\.dev/dl/go|setup-go' "$tmpdir/j" | head -n 1 | cut -d: -f1)
    if [ "$uses_node" -gt 0 ] && { [ "$has_npm" -eq 0 ] || { [ -n "$first_prov" ] && [ "$first_prov" -gt "$first_use" ]; }; }; then
      echo "❌ $wf job '$job' runs node without npm ci/install earlier in the same job"
      fail=1
    fi
    if [ "$uses_npx" -gt 0 ] && [ "$has_node_setup" -eq 0 ]; then
      echo "❌ $wf job '$job' runs npx without actions/setup-node in the same job"
      fail=1
    fi
    if [ "$uses_go" -gt 0 ] && { [ "$has_go" -eq 0 ] || { [ -n "$first_prov" ] && [ "$first_prov" -gt "$first_use" ]; }; }; then
      echo "❌ $wf job '$job' runs go without a Go install earlier in the same job"
      fail=1
    fi
  done < "$tmpdir/jobs"
done

if [ "$fail" -ne 0 ]; then
  echo "Each CI job runs on its own runner: provision inside the job, never rely on another job's side effects."
  exit 1
fi
echo "✓ every CI job provisions its own toolchain and deps"
