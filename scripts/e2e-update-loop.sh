#!/usr/bin/env bash
# End-to-end check of the upgrade loop. Weekly, never per PR.
#
# Every command in the loop has unit tests against a stubbed network and a
# fixture tree: --check-update's two halves, migrate's range parser and its
# curated notes, the scaffold itself. What none of them covers is the
# COMPOSITION against reality — a real clone at a real tag, a marker written by
# a released binary, the real releases API, the real raw-file URLs. That is
# where release-process drift shows up: a tag naming change, a moved changelog
# path, a marker format that no longer round-trips.
#
# The scenario is the one #213 asked about: a project scaffolded by the previous
# release must say it is behind, and migrate must hand back the range that fixes
# it. A failure here means the loop is broken for users of that release.
#
# Requires go, git, curl, jq, and the network. It clones a tag and builds it, so
# it does not belong in ci.yml.
#
# Two properties that are easy to break while editing this file:
#
#   * The CLONE IS THE TEMPLATE. The installer transforms a tree in place
#     (requireScaffold demands go.mod and internal/server/chi.go), so the tree
#     checked out at $base is what gets scaffolded; the ldflags contribute only
#     the `scaffolded-from:` marker. Swapping the clone for a fixture would leave
#     the marker asserted against nothing.
#   * Every API call here is ANONYMOUS, on purpose. The installer caches its
#     lookup for a day and this script runs weekly, so the volume is a handful of
#     requests — nowhere near the 60/hour an unauthenticated caller gets. A token
#     would have to reach the installer through config (check-secrets-env forbids
#     ad-hoc secret env reads in Go, and it was right to flag this one), which is
#     not worth it at this volume. If a weekly run ever goes red on a rate limit,
#     that config change is the fix — not an env read bolted on here.
set -euo pipefail

repo="${REPO:-calionauta/gogogo}"
work="${WORKDIR:-$(mktemp -d)}"
keep="${KEEP:-0}"
cleanup() {
  if [ "$keep" = "1" ]; then
    printf '\nupdate-loop: kept %s\n' "$work"
    return
  fi
  rm -rf "$work"
}
trap cleanup EXIT

fail() {
  printf 'update-loop: FAIL: %s\n' "$*" >&2
  exit 1
}
step() { printf '\nupdate-loop: %s\n' "$*"; }

command -v jq >/dev/null || fail "jq is required"
command -v curl >/dev/null || fail "curl is required"
mkdir -p "$work"

step "resolving the two newest releases of $repo"
tags=$(curl -fsSL "https://api.github.com/repos/$repo/releases?per_page=5" |
  jq -r '.[].tag_name | select(length > 0)') || fail "releases API unreachable"
latest=$(printf '%s\n' "$tags" | sed -n 1p)
base=$(printf '%s\n' "$tags" | sed -n 2p)
[ -n "$base" ] || fail "need two releases to have a range; got latest=${latest:-none}"
[ "$base" != "$latest" ] || fail "the two newest releases are the same tag ($base)"
printf 'base=%s latest=%s\n' "$base" "$latest"

step "building gogogo as $base, the release the fixture tree will claim"
go build -ldflags "-X github.com/calionauta/gogogo/internal/installer.Version=$base" \
  -o "$work/gogogo" ./cmd/gogogo

step "scaffolding a tree from $base"
git clone -q --depth 1 --branch "$base" "https://github.com/$repo" "$work/proj"
(cd "$work" && ./gogogo --name proj --owner e2eorg --no-tui --yes \
  --features none --dir proj) || fail "scaffold from $base failed"

marker=$(sed -n 's/^scaffolded-from:[[:space:]]*//p' "$work/proj/AGENTS.md" | head -1)
[ "$marker" = "$repo@$base" ] ||
  fail "the scaffolded AGENTS.md records '$marker', want '$repo@$base'"
printf 'recorded base: %s\n' "$marker"

step "the tree must know it is behind"
set +e
"$work/gogogo" --check-update --dir "$work/proj" --format json \
  >"$work/update.json" 2>"$work/update.err"
code=$?
set -e
if [ "$code" -ne 3 ]; then
  cat "$work/update.json" "$work/update.err" >&2 || true
  fail "--check-update exited $code, want 3 for 'an update exists'"
fi
jq -e '.treeStatus == "behind"' "$work/update.json" >/dev/null ||
  fail "treeStatus is '$(jq -r '.treeStatus' "$work/update.json")', want behind"

step "migrate must hand back the range $base -> $latest"
"$work/gogogo" migrate --dir "$work/proj" --format json >"$work/migrate.json" ||
  fail "migrate failed"
want_from=${base#v}
got_from=$(jq -r '.from' "$work/migrate.json")
[ "$got_from" = "$want_from" ] ||
  fail "migrate read the base as '$got_from', want '$want_from'"
entries=$(jq -r '(.releases | length)' "$work/migrate.json")
[ "$entries" -ge 1 ] ||
  fail "migrate returned an empty range between $base and $latest"
gates=$(jq -r '(.gates | length)' "$work/migrate.json")
[ "$gates" -ge 1 ] || fail "migrate returned no proof gates"
newest=$(jq -r '.releases[0].version' "$work/migrate.json")
printf 'range: %s release(s), newest %s, %s gate(s)\n' "$entries" "$newest" "$gates"

step "the loop works: $base reports behind, and $latest is what it is handed"
