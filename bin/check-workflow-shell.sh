#!/usr/bin/env bash
# check-workflow-shell.sh — every `run: |` block in the workflows must at least
# PARSE. `bash -n` is the cheapest proof that the shell will start; it catches
# the quoting bug that shipped a release step nobody could run.
#
# Why the extraction is not plain awk: GitHub Actions strips the block's common
# leading indentation before running it (a YAML block scalar's indentation is
# structural, not content). A checker that keeps the indent validates a script
# the runner never sees. That matters for a heredoc inside a command
# substitution — `body=$(cat <<'EOF' … EOF)` — where a 10-space indent turns the
# terminator into literal text and bash 5 reports an unmatched `)` that the real
# step would never hit. Dedenting first is what makes this a check of the step.
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
workflows_dir="${1:-$here/../.github/workflows}"

if ! command -v bash >/dev/null 2>&1; then
  echo "⚠ bash not found; cannot verify the workflow shell blocks"
  exit 0
fi
if ! command -v python3 >/dev/null 2>&1; then
  echo "⚠ python3 not found; cannot verify the workflow shell blocks"
  exit 0
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fails=0
checked=0

for wf in "$workflows_dir"/*.yml "$workflows_dir"/*.yaml; do
  [ -f "$wf" ] || continue
  name="$(basename "$wf")"

  # Extract every `run: |` block, dedented the way the runner dedents it.
  # A block ends at the first non-blank line indented no deeper than the key.
  python3 - "$wf" "$tmp" <<'PY'
import sys, textwrap
from pathlib import Path

src, out = Path(sys.argv[1]), Path(sys.argv[2])
lines = src.read_text().splitlines()
blocks, cur, keyindent = [], None, None
for line in lines:
    stripped = line.strip()
    lead = len(line) - len(line.lstrip())
    if cur is None:
        if stripped in ("run: |", "run: |-", "run: |+"):
            cur, keyindent = [], lead
        continue
    if not stripped:
        cur.append("")
        continue
    if lead > keyindent:
        cur.append(line)
        continue
    blocks.append(cur)
    cur = None
    if stripped in ("run: |", "run: |-", "run: |+"):
        cur, keyindent = [], lead
if cur is not None:
    blocks.append(cur)

for i, block in enumerate(blocks, 1):
    body = textwrap.dedent("\n".join(block)) + "\n"
    (out / f"block-{i}.sh").write_text(body)
PY

  for f in "$tmp"/block-*.sh; do
    [ -f "$f" ] || continue
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
