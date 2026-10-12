#!/bin/bash
# check-dagnats-pin.sh — docs/dagnats.md names the DagNats version its
# capability table was verified against. A `go get` bump that moves go.mod
# without updating the table ships stale bounds (MaxSteps, planner limits,
# new primitives) while the doc still claims the old pin.
#
# Pure local check (grep on go.mod + docs/dagnats.md): deterministic, no
# network, safe in pre-commit and CI alike. On mismatch it fails with the
# update procedure — diff upstream CHANGELOG.md between the two tags,
# refresh the capability table, move the pin, all in one commit.
set -uo pipefail

want=$(grep -E 'github.com/danmestas/dagnats v[0-9]' go.mod | head -n 1 | grep -oE 'v[0-9][0-9A-Za-z.+-]*' | head -n 1)
have=$(grep -E '^Pinned: DagNats v[0-9]' docs/dagnats.md | head -n 1 | grep -oE 'v[0-9][0-9A-Za-z.+-]*' | head -n 1)

if [ -z "$want" ]; then
    echo "check-dagnats-pin: no dagnats require found in go.mod"
    exit 1
fi
if [ -z "$have" ]; then
    echo "check-dagnats-pin: no '^Pinned: DagNats v...' line in docs/dagnats.md"
    exit 1
fi
if [ "$want" != "$have" ]; then
    echo "check-dagnats-pin: go.mod pins $want but docs/dagnats.md claims $have"
    echo "  1. diff upstream CHANGELOG.md between the tags"
    echo "  2. update the capability table in docs/dagnats.md"
    echo "  3. move the pin — all in one commit"
    exit 1
fi
echo "check-dagnats-pin: docs/dagnats.md matches go.mod ($want)"
