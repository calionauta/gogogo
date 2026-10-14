#!/bin/bash
# go-mod-download — warm the module cache with bounded retries.
#
# Why: GitHub runners start with a cold module cache (actions/cache stays
# off by design — flaky tar restores, see docs/local-ci.md), so every Go
# job downloads the full graph from proxy.golang.org, and the proxy answers
# cold-download storms with transient HTTP/2 INTERNAL_ERRORs (observed twice
# in one day: flatdeps `go list` dying after 156s, race `go test` naming the
# dagnats/goakt zips). The go command does not retry those itself — the step
# just dies mid-suite. This loop retries the download phase only (3 attempts,
# 15s backoff): a persistent outage still fails fast instead of dying 10
# minutes into the race suite, and a transient one never reaches the tests.
# Bash-3 safe (macOS ships 3.2: seq, no mapfile).
set -uo pipefail

tries=3
wait_s=15
for i in $(seq 1 "$tries"); do
  if go mod download all 2>&1; then
    echo "✓ module cache warm (attempt $i)"
    exit 0
  fi
  echo "⚠️ module download failed (attempt $i/$tries)"
  [ "$i" -lt "$tries" ] && sleep "$wait_s"
done
echo "❌ module downloads failed after $tries attempts"
exit 1
