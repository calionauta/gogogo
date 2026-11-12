#!/usr/bin/env bash
# A release must SHIP: the released code AND the released version, in
# production, every time.
#
# Why this is a check and not a comment: the guarantee lived in comments
# that contradicted each other. The tag-push arm said "a tag always
# deploys"; the schedule block said "tag pushes made by release-please do
# not start workflow runs". Both were true — and together they meant a
# release-please release deployed NOTHING. The release commit touches only
# CHANGELOG.md and the version manifest, neither of which is in the deploy
# allow-list, so the push arm did not fire either; the tag was pushed with
# GITHUB_TOKEN, which starts no workflow run at all. Production then served
# the new code labelled with the PREVIOUS version (v0.69.0's code, 0.68.0's
# badge) until the next code push. The schedule was supposed to heal it,
# but GitHub runs schedules on its own cadence — observed 4 to 10 hours
# apart, not every 30 minutes — so "the badge is right eventually" was the
# best the pipeline promised.
#
# Four facts make the guarantee true, and this check holds all four:
#
#   1. the release DISPATCHES the deploy (the release trigger — the same
#      workflow_dispatch bridge release.yml already uses for the installer
#      binaries, which had the very same bug);
#   2. the deploy's version comes from the tag POINTING AT the built commit,
#      not from the nearest tag;
#   3. the schedule exists (the safety net for a missed trigger);
#   4. workflow_dispatch exists on the deploy (the manual way to ship).
#
# The check reads the two workflow files; it does not talk to GitHub, does
# not deploy anything, and needs no network.
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
deploy="${1:-$here/../.github/workflows/deploy.yml}"
release="${2:-$here/../.github/workflows/release-please.yml}"

fails=0
require() { # file, pattern, description
  if grep -qE -- "$2" "$1"; then
    printf 'ok    %s\n' "$3"
  else
    printf 'FAIL  %s\n        %s does not match: %s\n' "$3" "$1" "$2"
    fails=$((fails + 1))
  fi
}

require "$release" 'gh workflow run deploy\.yml' \
  'the release dispatches the deploy (the release trigger)'
require "$release" "release_created == 'true'" \
  'the dispatch happens when a release was actually cut'
require "$deploy" 'git tag --points-at' \
  'the version comes from the tag pointing at the built commit'
require "$deploy" '^\s*schedule:' \
  'the scheduled reconcile exists (the safety net)'
require "$deploy" '^\s*workflow_dispatch:' \
  'workflow_dispatch exists (the manual release path)'

if [ "$fails" -ne 0 ]; then
  printf '\n%d release-deploy guarantee(s) broken\n' "$fails"
  exit 1
fi
printf 'release-deploy guarantees hold\n'
