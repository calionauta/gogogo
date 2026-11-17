#!/usr/bin/env bash
# The docs site publishes from master; this says whether it has caught up.
#
# Why this is a check and not a glance at the URL: "is the page updated?" was
# answered by reading the rendered HTML and diffing headings by hand, which
# works once and tells you nothing the next time. Worse, the answer looked fine
# for the wrong reason — a deploy run cancelled by the concurrency group (a
# later push supersedes it) reads as "not deployed", when in fact the LATER run
# published both changes. The deployment record carries the commit, so the
# question becomes arithmetic instead of inspection.
#
# Usage:
#   scripts/check-site-deployed.sh              # is the site on master's tip?
#   scripts/check-site-deployed.sh --sha <sha>  # is that commit published?
#   scripts/check-site-deployed.sh --quiet      # exit code only
#
# Exit 0 = published, 1 = behind, 2 = cannot tell (no network, no gh auth).
#
# NOT in ci-local: it needs network and a gh token, and a docs push is not a
# reason to block a test run. Run it after a merge that touches docs/ or site/.
set -uo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root" || exit 2

quiet=0
want=""
while [ $# -gt 0 ]; do
  case "$1" in
    --quiet) quiet=1 ;;
    --sha) shift; want="${1:-}" ;;
    -h|--help) sed -n '2,24p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
  shift
done

say() { [ "$quiet" = 1 ] || echo "$@"; }

command -v gh >/dev/null 2>&1 || { say "check-site-deployed: gh not installed — skipped"; exit 2; }
gh auth status >/dev/null 2>&1 || { say "check-site-deployed: gh not authenticated — skipped"; exit 2; }

repo=$(gh repo view --json nameWithOwner --jq .nameWithOwner 2>/dev/null) || {
  say "check-site-deployed: cannot resolve the repo — skipped"; exit 2
}

if [ -z "$want" ]; then
  git fetch -q origin master 2>/dev/null || true
  want=$(git rev-parse origin/master 2>/dev/null) || {
    say "check-site-deployed: cannot resolve origin/master — skipped"; exit 2
  }
fi
want_short=$(git rev-parse --short=8 "$want" 2>/dev/null || echo "${want:0:8}")

# The newest deployment that actually succeeded. A superseded run leaves an
# `inactive` record, so reading the most recent record (whatever its state)
# would report the wrong commit right after two pushes in a row.
api="repos/$repo/deployments?environment=github-pages&per_page=20"
read -r live_id live_sha <<EOF
$(gh api "$api" --jq '[.[] | select(.id)] | .[0] | "\(.id) \(.sha)"' 2>/dev/null)
EOF
[ -n "${live_id:-}" ] || { say "check-site-deployed: no Pages deployment found — skipped"; exit 2; }

# Newest status of every recent deployment, then keep the successful ones.
live_sha=""
for id in $(gh api "$api" --jq '.[].id' 2>/dev/null); do
  state=$(gh api "repos/$repo/deployments/$id/statuses" --jq '.[0].state' 2>/dev/null)
  if [ "$state" = "success" ]; then
    live_sha=$(gh api "repos/$repo/deployments/$id" --jq .sha 2>/dev/null)
    break
  fi
done
[ -n "$live_sha" ] || { say "check-site-deployed: no successful Pages deployment — skipped"; exit 2; }

live_short=${live_sha:0:8}

if [ "$live_sha" = "$want" ]; then
  say "✅ docs site is on $want_short (published commit matches)"
  exit 0
fi

# A later commit that touches neither docs/ nor site/ does not need a publish:
# the workflow only triggers on those paths, so being behind is correct.
behind=$(git rev-list "$live_sha..$want" -- docs site 2>/dev/null | wc -l | tr -d ' ')
if [ "$behind" = "0" ]; then
  say "✅ docs site is on $live_short; $want_short touches neither docs/ nor site/ (no publish owed)"
  exit 0
fi

say "❌ docs site is on $live_short but master is $want_short ($behind doc/site commit(s) unpublished)"
say "   watch it:  gh run list --workflow=pages.yml --limit 3"
say "   or force:  gh workflow run pages.yml"
exit 1
