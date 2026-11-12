#!/usr/bin/env bash
# Contract test for bin/check-deploy-release.sh.
#
# The check guards a promise ("production runs the release; the badge says
# what runs"), so the check itself needs a promise: it must FAIL when the
# guarantee is removed. A check that only ever passes is decoration.
#
# Each case removes ONE fact from the real workflows — production following
# master, the release trigger, the tag in the dispatch, the checkout of the
# release, the version, the net, the manual path — and the check must catch
# it. The real workflows must pass.
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

# production following master again: the push trigger grows a branches key
python3 - "$real_deploy" "$tmp/pushes.yml" <<'PY2'
import sys
src, dst = sys.argv[1], sys.argv[2]
text = open(src).read()
# The push trigger that once shipped master HEAD, the regression the model
# forbids: a tag-only push trigger becomes "master too".
text = text.replace('  push:\n    tags: ["v*"]', "  push:\n    branches: [master]\n    tags: [\"v*\"]", 1)
open(dst, "w").write(text)
PY2
expect 1 "production following master is caught" "$tmp/pushes.yml" "$real_release"

# the push trigger in the wrong SHAPE (tags as a sibling of push: is not a
# valid trigger - GitHub refuses to parse the workflow)
python3 - "$real_deploy" "$tmp/bad-shape.yml" <<'PY2'
import sys
src, dst = sys.argv[1], sys.argv[2]
text = open(src).read()
text = text.replace('  push:\n    tags: ["v*"]', '  tags: ["v*"]', 1)
open(dst, "w").write(text)
PY2
expect 1 "a push trigger in the wrong shape is caught" "$tmp/bad-shape.yml" "$real_release"

# the release no longer triggers the deploy
strip 'gh workflow run deploy\.yml' "$real_release" "$tmp/no-trigger.yml"
expect 1 "a release that cannot trigger the deploy is caught" \
  "$real_deploy" "$tmp/no-trigger.yml"

# the dispatch without the tag
strip 'deploy\.yml.*-f tag=' "$real_release" "$tmp/no-tag-arg.yml"
expect 1 "a dispatch that does not name the tag is caught" \
  "$real_deploy" "$tmp/no-tag-arg.yml"

# the dispatch firing on every push
strip "release_created == 'true'" "$real_release" "$tmp/no-condition.yml"
expect 1 "a dispatch that fires on every push is caught" \
  "$real_deploy" "$tmp/no-condition.yml"

# the build checking out the event SHA instead of the release
strip 'ref: \$\{\{ needs\.reconcile\.outputs\.ref \}\}' "$real_deploy" "$tmp/no-ref.yml"
expect 1 "a build that ignores the release is caught" \
  "$tmp/no-ref.yml" "$real_release"

# the version not coming from the release
strip 'version="\$\{\{ needs\.reconcile\.outputs\.version \}\}"' "$real_deploy" "$tmp/no-version.yml"
expect 1 "a version that is not the release is caught" \
  "$tmp/no-version.yml" "$real_release"

# the net and the manual path
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
