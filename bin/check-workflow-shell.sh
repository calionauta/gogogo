#!/usr/bin/env bash
# Every shell block in the workflows must PARSE.
#
# Why: a workflow's shell is only ever validated when it RUNS — and a run that
# dies on a syntax error is a deploy that never happens, discovered in
# production. That is not hypothetical: a raw double quote inside the
# `ssh $DEPLOY_SSH_HOST "…"` block of deploy.yml closed the runner's string
# early, and every release dispatch failed with
#
#   bash: -c: line 14: syntax error: unexpected end of file
#
# …twice, because the PR's own CI was green: the workflow parsed as YAML, and
# nothing ran it until the deploy did. `bash -n` over each block is the
# cheapest possible proof that the shell will at least START.
#
# The check reads the workflow files as text (indentation, not YAML): a YAML
# parser is not available on every runner, and the block shape here is simple
# — a `run: |` line and the lines indented deeper than it.
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
workflows_dir="${1:-$here/../.github/workflows}"

if ! command -v bash >/dev/null 2>&1; then
  echo "⚠ bash not found; cannot verify the workflow shell blocks"
  exit 0
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fails=0
checked=0

for wf in "$workflows_dir"/*.yml "$workflows_dir"/*.yaml; do
  [ -f "$wf" ] || continue
  name="$(basename "$wf")"
  rm -f "$tmp"/block-*.sh
  awk -v out="$tmp" '
    # A shell block starts at "run: |" and continues while the lines are
    # indented DEEPER than the key that introduced it. The key indent is
    # remembered, so a sibling step at the same level ends the block.
    /^[ \t]*run:[ \t]*\|[ \t]*$/ {
      block++
      infile = out "/block-" block ".sh"
      match($0, /^[ \t]*/)
      keyindent = RLENGTH
      next
    }
    infile != "" {
      if ($0 ~ /^[ \t]*$/) { print > infile; next }
      match($0, /^[ \t]*/)
      if (RLENGTH > keyindent) { print > infile; next }
      infile = ""
    }
  ' "$wf"

  for f in "$tmp"/block-*.sh; do
    [ -f "$f" ] || continue
    # A block that is empty (the key alone) has nothing to parse.
    [ -s "$f" ] || { rm -f "$f"; continue; }
    checked=$((checked + 1))
    if ! out="$(bash -n "$f" 2>&1)"; then
      printf '❌ %s: a shell block does not parse:\n%s\n' "$name" "$out"
      fails=$((fails + 1))
    fi
    rm -f "$f"
  done
done

if [ "$fails" -ne 0 ]; then
  printf '\n%d workflow shell block(s) do not parse\n' "$fails"
  exit 1
fi
printf '✅ %d workflow shell block(s) parse\n' "$checked"
