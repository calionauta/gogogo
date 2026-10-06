#!/bin/bash
# install.sh guards — the bootstrap's PATH contract.
#
# install.sh had no test coverage at all before this, even though it is the
# very first thing a new user runs and it touches PATH. The reason it went
# unnoticed: a `go` symlink in $BIN_DIR is invisible until someone installs a
# system Go and wonders why an old toolchain wins.
#
# Two invariants, both cheap to assert against the script TEXT (no network, no
# toolchain download required):
#
#   1. install.sh must never plant a `go` entry inside $BIN_DIR. $BIN_DIR is on
#      PATH by design (the gogogo binaries live there), so any `go` there
#      shadows a system Go for every new shell and, in a directory that may be
#      shared, reads as "the CLI is named go".
#   2. the bootstrap must actually make the toolchain reachable — i.e. export
#      $GO_DIR/bin on PATH — and must check reachability before prepending, or
#      the check is trivially true and the hint never prints.
set -uo pipefail
cd "$(dirname "$0")/.."

fail=0
note() { printf '  %s\n' "$1"; }

echo "→ install.sh bootstrap guard"

# 1. No `go` symlink or copy into $BIN_DIR.
if grep -nE 'ln +-s[f]?[^|]*"\$BIN_DIR/go"|ln +-s[f]?[^|]*\$BIN_DIR/go|cp [^|]*\$BIN_DIR/go"' install.sh >/dev/null 2>&1; then
  echo "  ❌ install.sh creates a 'go' entry in \$BIN_DIR — it will shadow the system Go:"
  grep -nE 'ln +-s[f]?[^|]*\$BIN_DIR/go' install.sh | sed 's/^/     /'
  fail=1
else
  note "✅ no 'go' symlink into \$BIN_DIR"
fi

# 2. The bootstrap puts $GO_DIR/bin on PATH.
if grep -qE 'PATH="\$GO_DIR/bin:\$PATH"' install.sh; then
  note "✅ bootstrap exports \$GO_DIR/bin on PATH"
else
  echo "  ❌ bootstrap does not export \$GO_DIR/bin on PATH — a non-interactive"
  echo "     'gogogo ...' exec would not find the toolchain it just installed."
  fail=1
fi

# 3. Reachability is tested BEFORE prepending (otherwise always true).
if grep -qE 'already_on_path=1' install.sh; then
  note "✅ reachability checked before prepending to PATH"
else
  echo "  ❌ the PATH check runs after prepending — it can never be false, so the"
  echo "     'add this to your shell' guidance never prints."
  fail=1
fi

# 4. Syntax must be valid POSIX sh (the script is piped to `sh`).
if sh -n install.sh 2>/dev/null; then
  note "✅ POSIX sh syntax valid"
else
  echo "  ❌ install.sh is not valid POSIX sh:"
  sh -n install.sh 2>&1 | sed 's/^/     /'
  fail=1
fi

if [ "$fail" -ne 0 ]; then
  echo "install.sh guard FAILED"
  exit 2
fi
echo "  ✓ install.sh bootstrap contract holds"
