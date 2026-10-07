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

if ! which deadcode >/dev/null 2>&1; then
    echo "⚡ deadcode not installed (go install golang.org/x/tools/cmd/deadcode@latest) — skipping"
    exit 0
fi

echo "→ Running deadcode scan..."
# stdout = findings, stderr = tool diagnostics. Kept apart on purpose.
out_file=$(mktemp)
err_file=$(mktemp)
trap 'rm -f "$out_file" "$err_file"' EXIT

deadcode -test ./cmd/web/... ./features/... ./router/... ./internal/nats/... ./internal/queue/... \
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
    echo "     Fix: go install golang.org/x/tools/cmd/deadcode@latest"
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
