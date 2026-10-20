#!/bin/bash
# Dead-code scan — runs at pre-push, not pre-commit: it compiles every
# package and takes ~80s. internal/llm and internal/datastar are
# intentionally-provided library APIs (wire them when a feature needs
# them); deadcode would flag their not-yet-wired exports as noise.
#
# ADVISORY, but it must not lie about WHY it failed. The previous version did:
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
#
# So: separate the streams, detect the toolchain-mismatch signature, and say
# "could not run" instead of inventing a finding. A check that cannot run is a
# different fact from a check that found something, and conflating them is
# worse than not running it.
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

echo "→ Running deadcode scan..."
# stdout = findings, stderr = tool diagnostics. Kept apart on purpose.
out_file=$(mktemp)
err_file=$(mktemp)
trap 'rm -f "$out_file" "$err_file"' EXIT

go tool deadcode -test ./cmd/web/... ./features/... ./internal/nats/... ./internal/queue/... \
    >"$out_file" 2>"$err_file"
status=$?

err_text=$(cat "$err_file")

# A toolchain/type-check failure means the scan is meaningless, whatever the
# exit status. Detect the SIGNATURE explicitly rather than inferring from
# emptiness, and keep the two failure modes apart: a toolchain mismatch has a
# known fix, any other failure does not.
toolchain_mismatch=0
if printf '%s' "$err_text" | grep -qE 'requires newer Go version|application built with go[0-9]|unknown field .* in struct literal|could not import|no required module provides'; then
    toolchain_mismatch=1
fi

if [ "$toolchain_mismatch" -eq 1 ]; then
    echo "  ⚠️  deadcode could NOT run — the installed binary is older than the Go toolchain."
    echo "     This is a tool mismatch, NOT a dead-code finding."
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
