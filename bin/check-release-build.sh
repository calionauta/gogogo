#!/bin/bash
# release build guard — every binary the release SHIPS must build the way the
# release builds it.
#
# Why this exists: .goreleaser.yaml builds all three binaries with
# CGO_ENABLED=0, from one Linux runner, for linux/darwin/windows x amd64/arm64.
# Nothing in CI ever built that combination, so `cmd/gui` acquired a
# transitive cgo dependency (gui -> features/todo/handlers ->
# internal/recipe/register -> internal/collab -> loro-go, which needs cgo) and
# every release from v0.63.0 to v0.71.2 failed — nine releases with zero
# assets, while `curl … | sh` 404'd for the first line of the landing page.
#
# The local `go build ./...` cannot catch this: it builds with the host's
# cgo enabled, and macOS/linux differ. A build that only works with cgo is a
# build that does not ship.
#
# Cheap on purpose: the module cache is warm in CI (the step sits after "Warm
# module cache"), and each build reuses it. No network, no toolchain download,
# no artifact kept — -o /dev/null.
#
# Usage: bin/check-release-build.sh            # all three binaries, host-free
#        bin/check-release-build.sh --darwin   # add the darwin target (slow on a
#                                              # Linux runner: needs the darwin
#                                              # cgo-free toolchain only, so it
#                                              # is safe; windows is pure-Go too)
#
# RED proof (do this when the guard is added or edited):
#   revert the todo RecipeID move, run this script, watch it name cmd/gui.
set -uo pipefail
cd "$(dirname "$0")/.."

fail=0
echo "→ release build guard (goreleaser's own matrix and flags)"

# The binaries AND their flags come from .goreleaser.yaml, not from this file:
# a fourth build added there and forgotten here would be a guard with a hole,
# and a build that legitimately turns cgo ON must not be failed by a guard that
# hardcodes it off. So read `main:` and that build's own CGO_ENABLED.
PARSED=$(awk '
  /^builds:/                { inbuilds=1; next }
  inbuilds && /^[a-z]/      { inbuilds=0 }
  inbuilds && /^  - id:/    { if (main != "") print main "=" (cgo==""?"0":cgo); cgo=""; main="" }
  inbuilds && /main:/       { m=$0; sub(/.*main:[ ]*/, "", m); sub(/[ ]*#.*/, "", m); main=m }
  inbuilds && /CGO_ENABLED=/ { c=$0; sub(/.*CGO_ENABLED=/, "", c); sub(/[^0-9].*/, "", c); cgo=c }
  END                       { if (main != "") print main "=" (cgo==""?"0":cgo) }
' .goreleaser.yaml)

if [ -z "$PARSED" ]; then
  echo "❌ could not read any 'main:' entries from .goreleaser.yaml"
  exit 1
fi

# GOOS/GOARCH: the release ships linux, darwin and windows on amd64+arm64.
# One representative pair per OS is enough to catch a cgo leak — the
# dependency is per-package, not per-arch — but crossing all of them is
# pennies with a warm cache, so do the real thing.
OSES="linux darwin windows"
ARCHES="amd64 arm64"

for entry in $PARSED; do
  main="${entry%%=*}"
  cgo="${entry##*=}"
  for os in $OSES; do
    for arch in $ARCHES; do
      if out=$(CGO_ENABLED="$cgo" GOOS="$os" GOARCH="$arch" \
               go build -o /dev/null "$main" 2>&1); then
        continue
      fi
      fail=1
      printf '  ❌ %s %s/%s\n' "$main" "$os" "$arch"
      # Print the compiler's own words. A guard that says "failed" without
      # naming the package sends the next person to bisect nine releases.
      printf '%s\n' "$out" | sed 's/^/     /' | head -12
      break 3
    done
  done
done

if [ "$fail" -ne 0 ]; then
  echo
  echo "A release binary does not build with CGO_ENABLED=0."
  echo "The release cross-compiles from one Linux runner, so a dependency"
  echo "that needs cgo cannot ship. Either drop the cgo dependency from that"
  echo "binary's import graph (preferred — see cmd/gui's todo.RecipeID), or"
  echo "change .goreleaser.yaml to build it with cgo and accept a per-OS"
  echo "runner (the heavier answer)."
  exit 1
fi

echo "  ✓ every release binary builds for ${OSES// /,} × ${ARCHES// /,} with cgo off"
exit 0
