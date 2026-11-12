#!/usr/bin/env bash
# Contract test for bin/check-workflow-shell.sh.
#
# The check exists because a workflow's shell is only validated when it RUNS,
# and a deploy that dies on a syntax error is a deploy that never happens.
# So the check must FAIL on a broken block — a check that only ever passes
# would not have caught the bug it was written for.
#
# The fixture is a copy of the real deploy.yml with one raw double quote
# reintroduced inside the `ssh $DEPLOY_SSH_HOST "…"` argument: the exact
# shape that broke two release dispatches.
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
check="${here}/check-workflow-shell.sh"
real="${here}/../.github/workflows"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fails=0
expect() { # want_exit, description
  local want="$1" desc="$2"
  bash "$check" "$3" >/dev/null 2>&1
  local got=$?
  if [ "$got" -ne "$want" ]; then
    printf 'FAIL  %s\n        exit %s, want %s\n' "$desc" "$got" "$want"
    fails=$((fails + 1))
  else
    printf 'ok    %s\n' "$desc"
  fi
}

# The real workflows parse.
expect 0 "the real workflows parse" "$real"

# A workflow with a raw quote inside the ssh block must be caught.
mkdir -p "$tmp/broken"
python3 - "$real/deploy.yml" "$tmp/broken/deploy.yml" <<'PY2'
import sys
src, dst = sys.argv[1], sys.argv[2]
text = open(src).read()
# The regression: a raw double quote inside the double-quoted ssh argument
# closes it early and the rest of the script becomes syntax garbage.
text = text.replace(
    "              # first tag whose local copy differs (it would clobber the",
    '              # first tag whose local copy differs ("would clobber the', 1)
open(dst, "w").write(text)
PY2
expect 1 "a workflow whose shell block does not parse is caught" "$tmp/broken"

if [ "$fails" -ne 0 ]; then
  printf '\n%d contract failure(s)\n' "$fails"
  exit 1
fi
printf 'workflow-shell contract holds\n'
