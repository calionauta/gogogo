#!/bin/bash
if [ ! -f go.mod ]; then
  echo "→ not a Go project, skipping govulncheck"
  exit 0
fi
echo "→ govulncheck (${GOVULNCHECK_WHEN:-pre-push})..."
# `go tool govulncheck`, not `go install ...@latest`: the scanner version is
# pinned by the `tool` directive in go.mod, so this gate runs the version the
# repo declares. Two reasons, both learned the hard way:
#
#   1. An unpinned scanner is an unpinned attack surface inside the step that
#      decides whether a push is safe. The AGENTS.md rule ("NO
#      `go install <tool>@latest` for the gates") exists because an unpinned
#      tool can be OLDER than the toolchain and then misread the module
#      graph — deadcode once reported type-check errors as dead code that way.
#   2. `go tool` builds the scanner from the module cache, so there is nothing
#      to install and nothing to keep warm, and the scanner can never disagree
#      with the toolchain about the module (the same reason `deadcode` and
#      `templ` are pinned this way).
# Bound the scanner ONLY on a box that cannot carry it; a capable machine
# (the developer's laptop, a CI runner) runs it unbounded. This is a security
# gate, so it is never skipped for capacity the way deadcode is — the worst a
# constrained host gets is a smaller heap and fewer procs, which only makes the
# scan slower. GOVULNCHECK_MIN_RAM_MB=6144 is the same floor the full gate uses.
if [ -r /proc/meminfo ]; then
  mem_avail_mb=$(awk '/^MemAvailable:/{printf "%d", $2/1024}' /proc/meminfo)
  if [ "${mem_avail_mb:-0}" -lt "${GOVULNCHECK_MIN_RAM_MB:-6144}" ]; then
    cap_mb=$(( mem_avail_mb / 2 ))
    [ "$cap_mb" -gt 2048 ] && cap_mb=2048
    [ "$cap_mb" -lt 512 ] && cap_mb=512
    export GOMEMLIMIT="${cap_mb}MiB"
    export GOMAXPROCS=2
    echo "  (bounded: ${cap_mb}MiB heap / 2 procs — ${mem_avail_mb}MB available)"
  fi
fi

PKGS=$(bash scripts/web-packages.sh)
go tool govulncheck $PKGS || { echo "❌ Vulnerability scan failed"; exit 1; }
echo "  ✓"
