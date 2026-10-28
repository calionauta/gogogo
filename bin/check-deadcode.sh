#!/bin/bash
# Dead-code scan — runs at pre-push, not pre-commit: it compiles every
# package. internal/llm and internal/datastar are intentionally-provided
# library APIs (wire them when a feature needs them); deadcode would flag
# their not-yet-wired exports as noise.
#
# ADVISORY, but it must not lie about WHY it failed. An earlier version did:
# it captured stderr with `2>&1` and printed anything non-empty under
# "⚠️ Dead code found". When the installed `deadcode` binary is older than the
# Go toolchain (it is a `go install`ed tool pinned at build time, while the
# toolchain moves), it emits type-check errors like:
#
#   package requires newer Go version go1.27 (application built with go1.26)
#   unknown field rfd in struct literal of type splicePipe
#
# ...for files in /usr/local/go/src. Those are TOOLCHAIN ERRORS, not findings,
# and reporting them as "dead code found" sends the reader hunting for dead
# functions that do not exist. This is exactly what happened on this host.
# So the two streams stay apart and a failure names its own cause.
#
# SIZED TO THE HOST (measured 2026-10 on this repo, idle box):
#   30s wall, 175% CPU, PEAK RSS 3.28 GB — more than a full-repo
#   golangci-lint (2.6 GB). On a shared box already running other agents,
#   committing 3.3 GB makes the scan swap and hang: a real push sat in this
#   hook for 23 minutes at load 18 on 4 cores and had to be retried. So the
#   gate now asks whether THIS machine can carry it right now — same
#   evidence-over-labels approach as scripts/lint-safe.sh and
#   scripts/check-host-capacity.sh — and refuses (advisory, exit 0) when it
#   cannot, instead of wedging the box and blocking the push.
#
# Overrides (env):
#   DEADCODE_FORCE=1        run anyway, even on a box that cannot carry it
#   DEADCODE_MIN_RAM_MB=N   MemAvailable floor (default 3584)
#   DEADCODE_MAX_LOAD_PER_CORE=N  load5/core ceiling (default 1.0)
#   DEADCODE_MAX_SWAP_PCT=N swap-in-use ceiling (default 50)
set -uo pipefail

# The scan patterns are module-relative, so resolve the repo root first: the
# hook runs from the worktree root, but a bare `bash bin/check-deadcode.sh`
# from bin/ (or a test harness) must behave identically. Same approach as
# scripts/lint-safe.sh.
cd "$(git rev-parse --show-toplevel)" || {
    echo "  ⚠️  not inside a git worktree — deadcode scan skipped"
    exit 0
}

# The tool is pinned by the `tool` directive in go.mod and run through
# `go tool`, so the scan can never disagree with the toolchain about which
# module version it is type-checking. That mismatch is what used to surface as
# "dead code found" — type-check errors printed under findings. With the pin,
# the toolchain-mismatch branch below should be unreachable; it stays as a
# guard for an exotic host (GOTOOLCHAIN switched mid-run, corrupt cache).
# `go tool -n <name>` prints the resolved binary path. It exits 0 even for an
# unknown tool (the message goes to stderr), so the probe is "did it print a
# path", not the exit code.
if [ -z "$(go tool -n deadcode 2>/dev/null)" ]; then
    echo "⚡ deadcode unavailable (not resolvable via 'go tool') — skipping"
    echo "   Pinned by the tool directive in go.mod; run \`go mod download\`."
    exit 0
fi

# --- capability probe: measured, never assumed -------------------------------
MIN_RAM_MB=${DEADCODE_MIN_RAM_MB:-3584}   # 3.28 GB measured peak + headroom
MAX_LOAD_PER_CORE=${DEADCODE_MAX_LOAD_PER_CORE:-1.0}

mem_avail_mb=0
load5=0
swap_used_pct=0
if [ -r /proc/meminfo ]; then
    mem_avail_mb=$(awk '/^MemAvailable:/{print int($2/1024)}' /proc/meminfo)
    load5=$(awk '{print $2}' /proc/loadavg)
    swap_used_pct=$(awk '/^SwapTotal:/{t=$2} /^SwapFree:/{f=$2} END{if(t>0) printf "%d", ((t-f)*100)/t; else print 0}' /proc/meminfo)
elif command -v sysctl >/dev/null 2>&1; then
    # macOS/BSD: no cheap "available" counter, use total (same compromise as
    # lint-safe.sh) and let the ceiling do the bounding.
    mem_avail_mb=$(( $(sysctl -n hw.memsize) / 1024 / 1024 ))
    load5=$(sysctl -n vm.loadavg | awk '{print $2}')
fi
cores=$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 2)

load_per_core_x100=$(awk -v l="$load5" -v c="$cores" 'BEGIN{printf "%d", (l/c)*100}')
max_x100=$(awk -v m="$MAX_LOAD_PER_CORE" 'BEGIN{printf "%d", m*100}')

refuse=""
if [ "$mem_avail_mb" -lt "$MIN_RAM_MB" ]; then
    refuse="RAM: ${mem_avail_mb} MB available < ${MIN_RAM_MB} MB required (measured peak 3.28 GB)"
elif [ "$load_per_core_x100" -gt "$max_x100" ]; then
    refuse="load: 5-min avg per core ${load5}/${cores} > ${MAX_LOAD_PER_CORE} (box already saturated)"
elif [ "$swap_used_pct" -ge "${DEADCODE_MAX_SWAP_PCT:-50}" ]; then
    # The 5-min load average lags a box that just got busy, and MemAvailable
    # can look healthy while swap is already carrying the working set. Two
    # outages on this host (2026-10-28) were swap exhaustion, and this scan
    # adds ~2.5 GB: on a box with swap half full that is a thrash, not a scan.
    refuse="swap: ${swap_used_pct}% used (>= ${DEADCODE_MAX_SWAP_PCT:-50}%) — adding 2.5GB would page, not scan"
fi

if [ -n "$refuse" ] && [ "${DEADCODE_FORCE:-0}" != "1" ]; then
    echo "  ⚡ deadcode SKIPPED — this box cannot carry it right now"
    echo "     ${refuse}"
    echo "     Not a finding and not a failure: the scan is advisory and the"
    echo "     push proceeds. CI runs it with dedicated capacity."
    echo "     Force with DEADCODE_FORCE=1 (expect swap on an undersized box)."
    exit 0
fi

echo "→ Running deadcode scan..."
# stdout = findings, stderr = tool diagnostics. Kept apart on purpose.
out_file=$(mktemp)
err_file=$(mktemp)
trap 'rm -f "$out_file" "$err_file"' EXIT

# Bound the GC and the parallelism to what was just probed, so the scan cannot
# balloon past its measured footprint and take the box with it.
cap_mb=$(( mem_avail_mb * 3 / 4 ))
[ "$cap_mb" -gt 4096 ] && cap_mb=4096
[ "$cap_mb" -lt 1024 ] && cap_mb=1024
export GOMEMLIMIT="${cap_mb}MiB"
gomaxprocs=2
[ "$cores" -lt 2 ] && gomaxprocs=1
export GOMAXPROCS="$gomaxprocs"

prefix=()
command -v nice >/dev/null 2>&1 && prefix+=(nice -n 15)
command -v ionice >/dev/null 2>&1 && prefix+=(ionice -c2 -n7)

"${prefix[@]}" go tool deadcode -test ./cmd/web/... ./features/... ./internal/nats/... ./internal/queue/... \
    >"$out_file" 2>"$err_file"
status=$?

err_text=$(cat "$err_file")

# A toolchain/type-check failure means the scan is meaningless, whatever the
# exit status. Detect the SIGNATURE explicitly rather than inferring from
# emptiness, and keep the two failure modes apart: a toolchain mismatch has a
# known fix, any other failure does not.
toolchain_mismatch=0
if printf '%s' "$err_text" | grep -qE 'requires newer Go version|application built with go[0-9]|unknown field .* in struct literal|could not import|no required module provides|signal: killed'; then
    toolchain_mismatch=1
fi

if [ "$toolchain_mismatch" -eq 1 ]; then
    echo "  ⚠️  deadcode could NOT run — the installed binary is older than the Go toolchain,"
    echo "     or the scan was OOM-killed (GOMEMLIMIT was ${GOMEMLIMIT})."
    echo "     This is a tool/host limit, NOT a dead-code finding."
    printf '%s\n' "$err_text" | head -3 | sed 's/^/       /'
    echo "     The tool is pinned in go.mod; fix with \`go mod download\`."
    exit 0
fi

if [ "$status" -ne 0 ]; then
    # A failure we do not recognise. Say only what is known — do NOT guess at a
    # cause, which is how the old version turned an unrelated breakage into
    # "dead code found".
    echo "  ⚠️  deadcode exited $status — scan did not complete (advisory, not a finding)"
    printf '%s\n' "$err_text" | head -3 | sed 's/^/       /'
    exit 0
fi

findings=$(cat "$out_file")
if [ -n "$findings" ]; then
    echo "  ⚠️  Dead code found:"
    printf '%s\n' "$findings" | head -20 | sed 's/^/       /'
else
    echo "  ✓ no dead code"
fi
echo "  ✓ done (advisory only)"
exit 0
