#!/bin/bash
# bump-pele.sh — move the vendored Pele bundle to a newer release.
#
# Pele draws the mermaid fences in a Chat answer as themeable SVG. The bundle
# is vendored rather than fetched at runtime: the image is distroless, so "one
# binary, no external services" would be a lie otherwise. A pin nobody moves is
# the other lie, which is why .github/workflows/pele-auto-bump.yml runs this on
# a schedule and opens a PR with the result.
#
# This is the writable half of the pin. web/resources/pele_version_test.go is
# the check — the bundle's own banner and pinnedPele must agree — and this
# script moves both. The sizes the docs quote are NOT patched here: they are
# measured out of the files by `go run ./cmd/check-docs -write`, so there is one
# measurer instead of two.
#
# Usage:
#   scripts/bump-pele.sh            # the latest npm release
#   scripts/bump-pele.sh 1.10.0     # a specific version
#   scripts/bump-pele.sh --dry-run  # report what would change, write nothing
#
# Exit codes:
#   0  bumped, already current, or (with --dry-run) reported
#   1  could not resolve the release, the fetch failed, or the bytes are not
#      Pele's (an error page would otherwise be vendored as "the library")

set -uo pipefail

ASSET="web/resources/static/pele.global.js"
PIN_TEST="web/resources/pele_version_test.go"
REGISTRY="https://registry.npmjs.org/pele/latest"
CDN="https://cdn.jsdelivr.net/npm/pele"

DRY_RUN=0
WANT=""
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=1 ;;
    -h|--help) sed -n '2,26p' "$0"; exit 0 ;;
    -*) echo "bump-pele: unknown flag $arg" >&2; exit 1 ;;
    *) WANT="$arg" ;;
  esac
done

for tool in curl python3 go; do
  command -v "$tool" >/dev/null 2>&1 || { echo "bump-pele: $tool is required" >&2; exit 1; }
done

current=$(python3 - "$PIN_TEST" <<'PY'
import re, sys
raw = open(sys.argv[1]).read()
m = re.search(r'const pinnedPele = "([0-9][^"]*)"', raw)
print(m.group(1) if m else "")
PY
)
[ -n "$current" ] || { echo "bump-pele: cannot read pinnedPele from $PIN_TEST" >&2; exit 1; }

if [ -z "$WANT" ]; then
  WANT=$(curl -s --fail --max-time 25 "$REGISTRY" | python3 -c 'import json,sys; print(json.load(sys.stdin)["version"])') \
    || { echo "bump-pele: cannot resolve the latest release from $REGISTRY" >&2; exit 1; }
fi
[ -n "$WANT" ] || { echo "bump-pele: empty version" >&2; exit 1; }

TMP=$(mktemp)
trap 'rm -f "$TMP"' EXIT

if ! curl -s --fail --max-time 40 -o "$TMP" "$CDN@$WANT/dist/pele.global.js"; then
  echo "bump-pele: fetch failed ($CDN@$WANT)" >&2
  exit 1
fi
[ -s "$TMP" ] || { echo "bump-pele: fetched bundle is empty" >&2; exit 1; }

# The banner is the claim the pin test verifies; checking it here means a
# redirect to an HTML error page fails loudly instead of being vendored.
first=$(head -1 "$TMP")
case "$first" in
  "/*! Pele $WANT | MIT License |"*) ;;
  *) echo "bump-pele: fetched bytes are not Pele $WANT (banner: ${first:0:80})" >&2; exit 1 ;;
esac

if cmp -s "$TMP" "$ASSET"; then
  echo "bump-pele: already current at $current (upstream $WANT is identical)"
  exit 0
fi

echo "bump-pele: Pele $current -> $WANT"
echo "bump-pele: bundle $(wc -c < "$ASSET") -> $(wc -c < "$TMP") bytes"

if [ "$DRY_RUN" = "1" ]; then
  echo "bump-pele: dry run — nothing written"
  exit 0
fi

cp "$TMP" "$ASSET"

python3 - "$PIN_TEST" "$current" "$WANT" <<'PY'
import re, sys

pin_test, current, want = sys.argv[1:4]
raw = open(pin_test).read()
new, count = re.subn(r'const pinnedPele = "[^"]*"', f'const pinnedPele = "{want}"', raw, count=1)
if count != 1:
    sys.exit(f"bump-pele: could not find pinnedPele in {pin_test}")
open(pin_test, "w").write(new)
PY
[ $? -eq 0 ] || { echo "bump-pele: the bundle is written but the pin is not — run 'git checkout -- $ASSET' and fix the anchor" >&2; exit 1; }

# One measurer: the same command the gate runs. It rewrites the Client weight
# table cells and the landing spans from the bytes on disk.
if ! go run ./cmd/check-docs -write; then
  echo "bump-pele: go run ./cmd/check-docs -write failed — the docs still quote the old sizes" >&2
  exit 1
fi

echo "bump-pele: wrote $ASSET, $PIN_TEST, and the regenerated sizes"
echo "bump-pele: review with 'git diff', then run:"
echo "  go test ./web/resources/ -run 'TestPeleVersionIsPinned|TestDiagramHelperPresent'"
echo "  go run ./cmd/check-docs"
