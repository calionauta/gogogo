#!/usr/bin/env bash
# Production runs RELEASES: the version the site shows and the code the site
# runs must be the same thing.
#
# Why this is a check and not a comment: the guarantee lived in comments that
# contradicted each other, and the pipeline shipped the opposite of what they
# said. The tag-push arm said "a tag always deploys"; the schedule block said
# "tag pushes made by release-please do not start workflow runs". Both were
# true — and together they meant a release-please release deployed NOTHING:
# the release commit touches only CHANGELOG.md and the version manifest,
# neither of which was in the deploy allow-list, so the push arm did not fire
# either; the tag was pushed with GITHUB_TOKEN, which starts no workflow run
# at all. Production then served the new code labelled with the PREVIOUS
# version (v0.69.0's code, 0.68.0's badge) until the next code push. The
# schedule was supposed to heal it, but GitHub runs schedules on its own
# cadence — observed 4 to 10 hours apart, not every 30 minutes — so "the
# badge is right eventually" was the best the pipeline promised.
#
# Then the model itself was wrong: production shipped master HEAD and
# labelled it with the nearest tag, so the box could report "0.69.0" while
# running a commit that was never released. The badge lied by a release.
# Production now IS the release.
#
# Six facts make that true, and this check holds all six:
#
#   1. a push to master does NOT deploy (production follows the release);
#   2. the release DISPATCHES the deploy, with the tag (the release trigger —
#      the same workflow_dispatch bridge release.yml already uses for the
#      installer binaries, which had the very same bug);
#   3. the deploy BUILDS the tag it was asked for (the checkout follows the
#      release, not the event's SHA);
#   4. the version comes from that release, so the badge names what runs;
#   5. the schedule exists and reconciles against the LATEST TAG (the net
#      for a missed dispatch);
#   6. workflow_dispatch exists with a tag input (the manual path, and the
#      rollback: ship an older release the same way).
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
reject() { # file, pattern, description
  if grep -qE -- "$2" "$1"; then
    printf 'FAIL  %s\n        %s still matches: %s\n' "$3" "$1" "$2"
    fails=$((fails + 1))
  else
    printf 'ok    %s\n' "$3"
  fi
}

# 1. production does not follow master
reject "$deploy" '^  push:' \
  'a push to master does not deploy production (production runs the release)'
# 2. the release dispatches the deploy, with the tag
require "$release" 'gh workflow run deploy\.yml' \
  'the release dispatches the deploy (the release trigger)'
require "$release" 'deploy\.yml.*-f tag=' \
  'the dispatch names the tag (the release to ship)'
require "$release" "release_created == 'true'" \
  'the dispatch happens when a release was actually cut'
# 3. the deploy builds the release
require "$deploy" 'ref: \$\{\{ needs\.reconcile\.outputs\.ref \}\}' \
  'the build checks out the release, not the event SHA'
# 4. the version is the release
require "$deploy" 'version="\$\{\{ needs\.reconcile\.outputs\.version \}\}"' \
  'the version baked in is the release version'
# 5./6. the net and the manual path
require "$deploy" '^\s*schedule:' \
  'the scheduled reconcile exists (the net for a missed dispatch)'
require "$deploy" '^\s*workflow_dispatch:' \
  'workflow_dispatch exists (the manual path and the rollback)'
require "$deploy" '^\s*tag:' \
  'the dispatch takes a tag (ship an older release = rollback)'

if [ "$fails" -ne 0 ]; then
  printf '\n%d release-deploy guarantee(s) broken\n' "$fails"
  exit 1
fi
printf 'release-deploy guarantees hold\n'
