#!/usr/bin/env bash
# lint-safe.sh — run golangci-lint sized to what THIS machine can actually
# afford, decided from evidence (free RAM + cores) at run time. It never asks
# "is this a server or a laptop?" — that label is not the signal. The signal is
# whether the machine has the headroom to run the workload *right now*.
#
# Why this exists: a full-repo `golangci-lint run` of this module peaks around
# 2.6 GB RSS. On a host that is already running other work (a co-tenant daemon,
# production containers, a couple of coding agents) that spike pushes the box
# into swap and it stops answering. The fix is not to tune the GC of a bad run
# after it started; it is to (a) scope the run to what changed and (b) refuse
# the workload the machine cannot carry, before memory is committed.
#
# Decision — every branch is measured, not guessed:
#   * full repo   -> only if MemAvailable >= LINT_FULL_BUDGET_MB (default 4096)
#   * otherwise   -> changed packages only (scripts/changed-packages.sh)
#   * always      -> GOMEMLIMIT / --concurrency / nice sized from free RAM+cores
#   * always      -> inside a memory-capped cgroup when the platform offers one
#                    (systemd-run --scope), so a runaway lint dies in its own
#                    cgroup instead of waking the global OOM killer
#   * never       -> `golangci-lint cache clean` (the cache is what keeps a run
#                    cheap; clearing it re-type-checks the whole module cold)
#
# Overrides (env):
#   LINT_FULL=1            force a full-repo run (still memory-capped)
#   LINT_SCOPE=1           force a changed-packages run
#   LINT_FULL_BUDGET_MB=N  headroom required before a full run is auto-chosen
#   LINT_NO_CGROUP=1       skip the systemd-run scope even when available
#   LINT_CAP_MB=N          hard override for the memory ceiling
#
# Usage:
#   bash scripts/lint-safe.sh                       # scoped, or full if the host can take it
#   bash scripts/lint-safe.sh ./features/todo/...   # extra golangci-lint args pass through
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

# --- capability probe: measured, never assumed -------------------------------
mem_total_mb=0
mem_avail_mb=0
if [ -r /proc/meminfo ]; then
  mem_total_mb=$(awk '/^MemTotal:/{print int($2/1024)}' /proc/meminfo)
  mem_avail_mb=$(awk '/^MemAvailable:/{print int($2/1024)}' /proc/meminfo)
elif command -v sysctl >/dev/null 2>&1; then
  # macOS/BSD: hw.memsize is total; there is no cheap "available" counter, so
  # use total as the evidence and let the GOMEMLIMIT cap do the bounding.
  mem_total_mb=$(( $(sysctl -n hw.memsize) / 1024 / 1024 ))
  mem_avail_mb=$mem_total_mb
fi

cores=$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 2)

LINT_FULL_BUDGET_MB=${LINT_FULL_BUDGET_MB:-4096}

# --- choose scope from the evidence ------------------------------------------
run_full=0
if [ "${LINT_SCOPE:-0}" = "1" ]; then
  run_full=0
elif [ "${LINT_FULL:-0}" = "1" ]; then
  run_full=1
elif [ "$mem_avail_mb" -ge "$LINT_FULL_BUDGET_MB" ]; then
  run_full=1
fi

if [ "$run_full" = "1" ]; then
  pkgs=$(bash scripts/web-packages.sh)
  conc=$cores
else
  pkgs=$(bash scripts/changed-packages.sh)
  conc=$(( cores / 2 )); [ "$conc" -lt 1 ] && conc=1
fi

if [ -z "${pkgs//[[:space:]]/}" ]; then
  echo "lint-safe: no Go packages to lint"
  exit 0
fi

if ! command -v golangci-lint >/dev/null 2>&1; then
  echo "lint-safe: golangci-lint not installed (see docs/code-quality.md)" >&2
  exit 1
fi

# --- memory ceiling sized from the evidence ----------------------------------
# Full run: headroom was proven, so give it a generous slice. Scoped run: be
# frugal. GOMEMLIMIT is a soft cap — the GC gets aggressive as it approaches,
# which surfaces a problem as a slow/failed lint rather than a swap storm.
if [ -n "${LINT_CAP_MB:-}" ]; then
  cap_mb=$LINT_CAP_MB
elif [ "$run_full" = "1" ]; then
  cap_mb=$(( mem_avail_mb * 3 / 4 ))
else
  cap_mb=$(( mem_avail_mb / 2 ))
fi
[ "$cap_mb" -gt 8192 ] && cap_mb=8192
[ "$cap_mb" -lt 512 ] && cap_mb=512

export GOMEMLIMIT="${cap_mb}MiB"
export GOMAXPROCS="$conc"
export GOGC="${GOGC:-80}"

scope_label=$([ "$run_full" = "1" ] && echo full || echo changed)
printf 'lint-safe: %s | %s/%s MB free | %s cores | conc=%s | GOMEMLIMIT=%s\n' \
  "$scope_label" "$mem_avail_mb" "$mem_total_mb" "$cores" "$conc" "$GOMEMLIMIT"

cmd=(golangci-lint run --concurrency "$conc" --timeout 5m $pkgs "$@")

prefix=(nice -n 15)
command -v ionice >/dev/null 2>&1 && prefix+=(ionice -c2 -n7)

# --- hard isolation when the platform can provide it -------------------------
# A transient user scope with MemoryMax means a runaway lint is OOM-killed
# inside its own cgroup; the rest of the machine keeps answering.
if [ "${LINT_NO_CGROUP:-0}" != "1" ] \
  && command -v systemd-run >/dev/null 2>&1 \
  && systemd-run --user --scope --quiet true >/dev/null 2>&1; then
  exec "${prefix[@]}" systemd-run --user --scope --quiet \
    -p MemoryMax="${cap_mb}M" \
    -p MemorySwapMax=256M \
    -p CPUQuota=$(( conc * 100 ))% \
    -p Nice=10 \
    "${cmd[@]}"
fi

exec "${prefix[@]}" "${cmd[@]}"
