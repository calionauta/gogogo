#!/usr/bin/env bash
# check-host-capacity.sh — pre-flight for `make ci-local` (the T4 gate).
#
# Why: T4 runs full-repo lint + the full race suite + four parallel
# Playwright suites. Measured 5:50–7:11 on a 10-core Mac; on a small
# shared host the same workload pins CPU at 99% and the box stops
# answering (production and co-tenant agents starve), or dies weirdly
# at 100% disk (link failures, not test failures). This script measures
# the machine and refuses the workload it cannot carry, before anything
# is committed — the same evidence-over-labels approach as
# scripts/lint-safe.sh, one level up (whole gate, not just lint).
#
# Checks (every branch measured; thresholds overridable via env):
#   RAM:    MemAvailable >= CI_LOCAL_MIN_RAM_MB (default 6144).
#           T4 stacks the lint peak (~2.6 GB RSS) with the race suite
#           plus three Chromium instances, so it needs headroom well
#           above the lint-only budget (4096).
#   CPU:    cores >= CI_LOCAL_MIN_CORES (default 4) AND 5-min loadavg
#           per core <= CI_LOCAL_MAX_LOAD_PER_CORE (default 0.75).
#           The core floor exists because the fan-out runs four heavy
#           jobs; below it every browser wait flakes. The load ceiling
#           exists because a box ALREADY saturated (prod traffic, another
#           agent mid-run) must not take on T4 on top.
#   Disk:   free on the repo filesystem >= CI_LOCAL_MIN_DISK_MB
#           (default 2048). Near-full filesystems fail every step with
#           misleading errors (linker "no space left", not red tests).
#
# Overrides:
#   CI_LOCAL_FORCE=1   run anyway (prints the failed dimensions first).
#
# Usage: bash scripts/check-host-capacity.sh
#   Exit 0: the box can carry T4. Exit 1: it cannot — run
#   `make ci-local-fast` locally and let the PR's CI be the full gate.
set -euo pipefail

MIN_RAM_MB=${CI_LOCAL_MIN_RAM_MB:-6144}
MIN_CORES=${CI_LOCAL_MIN_CORES:-4}
MAX_LOAD_PER_CORE=${CI_LOCAL_MAX_LOAD_PER_CORE:-0.75}
MIN_DISK_MB=${CI_LOCAL_MIN_DISK_MB:-2048}

# --- capability probe: measured, never assumed -------------------------------
mem_total_mb=0
mem_avail_mb=0
load5=0
if [ -r /proc/meminfo ]; then
  mem_total_mb=$(awk '/^MemTotal:/{print int($2/1024)}' /proc/meminfo)
  mem_avail_mb=$(awk '/^MemAvailable:/{print int($2/1024)}' /proc/meminfo)
  # 4th field of /proc/loadavg is running/total; 5-min average is field 2.
  load5=$(awk '{print $2}' /proc/loadavg)
elif command -v sysctl >/dev/null 2>&1; then
  # macOS/BSD: hw.memsize is total; there is no cheap "available" counter,
  # so use total as the evidence (same compromise as lint-safe.sh).
  mem_total_mb=$(( $(sysctl -n hw.memsize) / 1024 / 1024 ))
  mem_avail_mb=$mem_total_mb
  # vm.loadavg prints "{ 1.20 0.80 0.50 }": 1/5/15 min.
  load5=$(sysctl -n vm.loadavg | awk '{print $2}')
fi

cores=$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 2)
disk_avail_mb=$(df -m . | awk 'NR==2{print $4}')

fail=0

echo "host capacity for make ci-local (T4): ${cores} cores, ${mem_avail_mb} MB avail / ${mem_total_mb} MB total, load5 ${load5}, disk ${disk_avail_mb} MB free"

if [ "$mem_avail_mb" -lt "$MIN_RAM_MB" ]; then
  echo "  ❌ RAM: ${mem_avail_mb} MB available < ${MIN_RAM_MB} MB required"
  fail=1
else
  echo "  ✓ RAM: ${mem_avail_mb} MB available (>= ${MIN_RAM_MB} MB)"
fi

if [ "$cores" -lt "$MIN_CORES" ]; then
  echo "  ❌ CPU: ${cores} cores < ${MIN_CORES} required (fan-out runs 4 heavy jobs)"
  fail=1
else
  echo "  ✓ CPU: ${cores} cores (>= ${MIN_CORES})"
fi

# Floating-point compare without awk-flavored surprises: scale by 100.
load_per_core_x100=$(awk -v l="$load5" -v c="$cores" 'BEGIN{printf "%d", (l/c)*100}')
max_x100=$(awk -v m="$MAX_LOAD_PER_CORE" 'BEGIN{printf "%d", m*100}')
if [ "$load_per_core_x100" -gt "$max_x100" ]; then
  echo "  ❌ load: 5-min avg per core ${load5}/${cores} > ${MAX_LOAD_PER_CORE} — box already saturated, T4 would wedge it"
  fail=1
else
  echo "  ✓ load: 5-min avg per core ${load5}/${cores} (<= ${MAX_LOAD_PER_CORE})"
fi

if [ "$disk_avail_mb" -lt "$MIN_DISK_MB" ]; then
  echo "  ❌ disk: ${disk_avail_mb} MB free < ${MIN_DISK_MB} MB required"
  fail=1
else
  echo "  ✓ disk: ${disk_avail_mb} MB free (>= ${MIN_DISK_MB} MB)"
fi

if [ "$fail" -ne 0 ]; then
  if [ "${CI_LOCAL_FORCE:-0}" = "1" ]; then
    echo "⚠️  CI_LOCAL_FORCE=1: running T4 on an undersized box anyway (expect swap/CPU wedge)"
    exit 0
  fi
  echo ""
  echo "✗ this box cannot carry make ci-local: run 'make ci-local-fast' locally"
  echo "  and let the PR's CI be the full gate. Override with CI_LOCAL_FORCE=1"
  echo "  (thresholds: CI_LOCAL_MIN_RAM_MB / CI_LOCAL_MIN_CORES /"
  echo "  CI_LOCAL_MAX_LOAD_PER_CORE / CI_LOCAL_MIN_DISK_MB)."
  exit 1
fi

echo "✅ host can carry make ci-local"
