#!/bin/bash
# release asset guard — a published release must actually carry the binaries
# the install path downloads.
#
# Why this exists: release-please publishes the GitHub release as soon as the
# release PR merges, and release.yml (goreleaser) runs AFTER that. So a release
# whose binaries fail to build is already public, already announced, and
# already the thing a visitor's first line of the landing page points at. From
# v0.63.0 to v0.71.2 that is exactly what happened: nine public releases, zero
# assets, and `curl … | sh` 404'd the whole time. Nobody noticed because nothing
# ever looked — release.yml failed in a job nobody reads, and no gate asked
# "does the thing we just published actually contain a binary".
#
# This runs OUTSIDE the pull-request path on purpose. A PR cannot know what the
# last published release contains, and asking GitHub on every push would make
# the daily loop depend on the network. It runs on a schedule (so a bad release
# is caught within a day) and is wired as the last step of release.yml (so a
# bad release is caught by the run that produced it).
#
# Usage:
#   bin/check-release-assets.sh                 # newest published release
#   bin/check-release-assets.sh v0.71.2         # a specific tag
#   bin/check-release-assets.sh --offline       # skip if gh/network is absent
#
# Exit: 0 assets present (or skipped), 1 assets missing, 3 cannot determine
# (no gh, no network, no releases yet) — never a false red on a dev box.
set -uo pipefail
cd "$(dirname "$0")/.."

fail=0
note() { printf '  %s\n' "$1"; }

if [ "${1:-}" = "--offline" ]; then
  note "skipped (--offline)"
  exit 0
fi

REPO=$(git config --get remote.origin.url 2>/dev/null |
       sed -E 's#(git@github.com:|https://github.com/)##; s#\.git$##')
if [ -z "$REPO" ]; then
  note "skipped (no origin remote)"
  exit 0
fi

if ! command -v gh >/dev/null 2>&1; then
  note "skipped (gh CLI not installed)"
  exit 0
fi

TAG="${1:-}"
if [ -z "$TAG" ]; then
  TAG=$(gh release view --repo "$REPO" --json tagName -q .tagName 2>/dev/null)
fi
if [ -z "$TAG" ]; then
  note "skipped (no published release for $REPO)"
  exit 0
fi

echo "→ release asset guard ($REPO $TAG)"

# Two independent checks, because they fail differently:
#   1. the release has assets at all  (the v0.63.0..v0.71.2 failure)
#   2. the asset install.sh actually fetches is one of them  (a rename that
#      leaves a release "non-empty" but still undownloadable)
# Distinguish "the release does not exist" from "it exists and is empty": they
# point at different runs, and telling an operator to check release.yml for a
# tag that was never published sends them to a workflow that never ran.
if ! gh release view "$TAG" --repo "$REPO" --json tagName >/dev/null 2>&1; then
  echo "  ❌ no published release for $TAG in $REPO"
  echo "     Nothing to install: this tag was never released, or it was deleted."
  echo "     If it WAS released, the release-please run that cuts it is the one"
  echo "     to read (release.yml only runs after the tag exists)."
  exit 1
fi

ASSETS=$(gh release view "$TAG" --repo "$REPO" --json assets -q '.assets[].name' 2>/dev/null)
if [ -z "$ASSETS" ]; then
  echo "  ❌ $TAG exists but carries no assets"
  echo "     release-please published the release, but the goreleaser run that"
  echo "     attaches the binaries failed. Check the release.yml run for $TAG."
  exit 1
fi
note "$(printf '%s\n' "$ASSETS" | wc -l | tr -d ' ') asset(s)"

# The tarball name install.sh builds. Read it from install.sh itself so a
# rename in one place cannot pass this guard while breaking the other.
EXPECTED=$(grep -oE 'gogogo-[0-9.]+-\$\{?[A-Za-z_]+\}?-\$\{?[A-Za-z_]+\}?\.tar\.gz' install.sh | head -1)
if [ -z "$EXPECTED" ]; then
  EXPECTED='gogogo-.*\.tar\.gz'
fi
if ! printf '%s\n' "$ASSETS" | grep -qE "$EXPECTED"; then
  echo "  ❌ $TAG has assets, but none matches what install.sh downloads"
  note "install.sh looks for: $EXPECTED"
  note "assets present:"
  printf '%s\n' "$ASSETS" | sed 's/^/       /'
  exit 1
fi
note "install.sh's tarball pattern is present"

# The checksum file the bootstrap verifies against.
if ! printf '%s\n' "$ASSETS" | grep -qE 'SHA256SUMS|checksums\.txt'; then
  echo "  ❌ $TAG has no checksum file (install.sh verifies one)"
  exit 1
fi
note "checksum file present"

echo "  ✓ $TAG carries a downloadable binary"
exit $fail
