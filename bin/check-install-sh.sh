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

# 3. The installer must hand over the MCP, or the front door stays unlabelled.
#
# The page and docs now lead with "wire the MCP", and install.sh installs
# gogogo-mcp on PATH — then used to say nothing about it, so the one next step
# nobody guesses went unprinted. Text-only, like the guards above: it must be
# PRINTED (never executed), because a script that edits a client's config file
# on its own breaks configurations it cannot see.
#
# Naming all four clients is the fix for the earlier form, which printed
# `claude ...` and deferred the rest to `docs/mcp.md`. A reader with Codex had
# to go find the file; a reader with Claude got a path they could not run.
for client in claude codex opencode pi; do
  if grep -qE "$client mcp add .*gogogo-mcp" install.sh; then
    note "✅ installer prints the $client wiring"
  else
    echo "  ❌ install.sh no longer prints '$client mcp add' — a $client user is"
    echo "     told to read a doc instead of being handed the command."
    fail=1
  fi
done

if grep -qE 'gogogo advise --need' install.sh; then
  note "✅ installer prints the no-agent CLI path"
else
  echo "  ❌ install.sh stopped printing the CLI fallback."
  fail=1
fi

# 4. The printed command must carry an ABSOLUTE path, never `~`.
#
# A client stores the command verbatim and spawns it without a shell, so `~`
# is never expanded: measured, `pi mcp add gogogo -- '~/.local/bin/gogogo-mcp'`
# stores the tilde and `pi mcp list` then reports
# "spawn /Users/you/.local/bin/gogogo-mcp ENOENT". The message looks like a
# missing binary rather than a quoting mistake, which is why the guard asserts
# on the text here instead of trusting the line to stay correct.
if grep -nE 'mcp add [^\n]*-- *~/' install.sh >/dev/null 2>&1; then
  echo "  ❌ install.sh tells the reader to wire the MCP with a literal '~':"
  grep -nE 'mcp add [^\n]*-- *~/' install.sh | sed 's/^/     /'
  echo "     clients spawn without a shell — print \$BIN_DIR or \$HOME instead"
  fail=1
else
  note "✅ MCP commands carry an absolute path, not '~'"
fi

# ... and it must not wire itself. Editing ~/.claude.json or a Codex TOML from
# an installer is the failure mode this note exists to prevent.
if grep -nE '^[^#]*(jq [^|]*(claude|cursor)\.json|\.codex/config\.toml)' install.sh >/dev/null 2>&1; then
  echo "  ❌ install.sh edits a client config file — print the command, never run it"
  fail=1
else
  note "✅ installer never edits a client config"
fi

exit $fail
