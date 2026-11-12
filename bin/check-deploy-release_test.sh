#!/usr/bin/env bash
# Contract test for bin/check-deploy-release.sh.
#
# The check guards a promise ("every release ships the released version"),
# so the check itself needs a promise: it must FAIL when the guarantee is
# removed. A check that only ever passes is decoration.
#
# Each case removes ONE fact from the real workflows — the release trigger,
# the tag resolution, the schedule, the manual path — and the check must
# catch it. The real workflows must pass.
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
check="${here}/check-deploy-release.sh"
real_deploy="${here}/../.github/workflows/deploy.yml"
real_release="${here}/../.github/workflows/release-please.yml"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fails=0
expect() { # want_exit, description, deploy, release
  local want="$1" desc="$2" deploy="$3" release="$4"
  bash "$check" "$deploy" "$release" >/dev/null 2>&1
  local got=$?
  if [ "$got" -ne "$want" ]; then
    printf 'FAIL  %s\n        exit %s, want %s\n' "$desc" "$got" "$want"
    fails=$((fails + 1))
  else
    printf 'ok    %s\n' "$desc"
  fi
}

# The real workflows satisfy every guarantee.
expect 0 "the real workflows satisfy the guarantees" "$real_deploy" "$real_release"

strip() { # pattern, source, destination
  grep -vE -- "$1" "$2" >"$3"
}

strip 'gh workflow run deploy\.yml' "$real_release" "$tmp/no-trigger-release.yml"
expect 1 "a release that cannot trigger the deploy is caught" \
  "$real_deploy" "$tmp/no-trigger-release.yml"

strip "release_created == 'true'" "$real_release" "$tmp/no-condition.yml"
expect 1 "a dispatch that fires on every push is caught" \
  "$real_deploy" "$tmp/no-condition.yml"

strip 'git tag --points-at' "$real_deploy" "$tmp/no-tag.yml"
expect 1 "a version that ignores the built commit's tag is caught" \
  "$tmp/no-tag.yml" "$real_release"

strip '^\s*schedule:' "$real_deploy" "$tmp/no-schedule.yml"
expect 1 "a missing safety-net schedule is caught" \
  "$tmp/no-schedule.yml" "$real_release"

strip '^\s*workflow_dispatch:' "$real_deploy" "$tmp/no-dispatch.yml"
expect 1 "a missing manual release path is caught" \
  "$tmp/no-dispatch.yml" "$real_release"

if [ "$fails" -ne 0 ]; then
  printf '\n%d contract failure(s)\n' "$fails"
  exit 1
fi
printf 'release-deploy contract holds\n'
