#!/usr/bin/env bash
# Contract test for bin/install-browser.sh.
#
# The script exists because the browser gates used to die on an apt/DNS
# problem that had nothing to do with the change under test (2026-11-11: the
# runners stopped resolving *.ubuntu.com; three PR runs failed in "Failed to
# install browsers"). The behaviour worth pinning is therefore the FALLBACK
# order, not the happy path:
#
#   * a broken apt must NOT take the browser down (the image's libraries are
#     enough) — and it must be loud about it;
#   * a broken browser download MUST take it down (there is no fallback);
#   * a browser that cannot LAUNCH must take it down, because every install
#     step above can "succeed" while the binary cannot start;
#   * skipping the deps on purpose must not silently call apt anyway.
#
# The stubs stand in for npx (the installer) and node (the launch check), so
# the test runs in a second and needs no browser, no apt and no network.
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
install="${here}/install-browser.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

stub_bin="$tmp/bin"
mkdir -p "$stub_bin"

cat >"$stub_bin/npx" <<'STUB'
#!/usr/bin/env bash
# Stub installer: records the call and fails according to the mode.
printf 'npx %s\n' "$*" >>"$STUB_LOG"
case "$*" in
*"install chromium"*)
	[ "${NPX_INSTALL_FAIL:-0}" = "1" ] && exit 1
	exit 0
	;;
*"install-deps"*)
	[ "${NPX_DEPS_FAIL:-0}" = "1" ] && exit 1
	exit 0
	;;
esac
exit 0
STUB

cat >"$stub_bin/node" <<'STUB'
#!/usr/bin/env bash
# Stub launch check: records the call and fails according to the mode.
printf 'node %s\n' "$*" >>"$STUB_LOG"
[ "${NODE_LAUNCH_FAIL:-0}" = "1" ] && exit 1
echo "chromium 999.0.0 launches"
exit 0
STUB

chmod +x "$stub_bin/npx" "$stub_bin/node"

fails=0

run_case() { # run_case <description> <expect-exit> <expect-log-substring> [env...]
	local desc="$1" want_exit="$2" want_log="$3"
	shift 3
	local log="$tmp/calls.log"
	: >"$log"
	local out
	out="$(env PATH="$stub_bin:$PATH" STUB_LOG="$log" PLAYWRIGHT_OS=Linux "$@" bash "$install" 2>&1)"
	local got=$?
	local ok=1
	if [ "$got" != "$want_exit" ]; then
		ok=0
	fi
	if [ -n "$want_log" ] && ! printf '%s\n' "$out" | grep -qF "$want_log"; then
		ok=0
	fi
	if [ "$ok" = "1" ]; then
		printf 'ok    %s\n' "$desc"
	else
		printf 'FAIL  %s\n        want exit %s and log containing %q, got exit %s\n' \
			"$desc" "$want_exit" "$want_log" "$got"
		printf '%s\n' "$out" | sed 's/^/        | /'
		fails=$((fails + 1))
	fi
	printf '%s' "$out" >"$tmp/last-output"
	cp "$log" "$tmp/last-calls"
}

echo "--- the happy path installs, deps first, and proves the launch ---"
run_case "everything available" 0 "OK: browser ready"
grep -q "npx playwright install chromium" "$tmp/last-calls" || {
	echo "FAIL  the browser binary was never installed"; fails=$((fails + 1))
}

echo
echo "--- a broken apt mirror must not take the browser down ---"
run_case "apt fails, the browser still launches" 0 \
	"WARNING: could not install the browser system dependencies" NPX_DEPS_FAIL=1

echo
echo "--- a broken browser download must take it down ---"
run_case "the browser binary cannot be installed" 1 \
	"ERROR: could not install the Chromium binary" NPX_INSTALL_FAIL=1

echo
echo "--- a browser that cannot launch must take it down ---"
run_case "the browser never launches" 1 \
	"ERROR: no usable Chromium" NODE_LAUNCH_FAIL=1

echo
echo "--- skipping the deps must not call apt anyway ---"
run_case "PLAYWRIGHT_SKIP_DEPS=1" 0 "OK: browser ready" PLAYWRIGHT_SKIP_DEPS=1
if grep -q "install-deps" "$tmp/last-calls"; then
	echo "FAIL  PLAYWRIGHT_SKIP_DEPS=1 still called install-deps"
	fails=$((fails + 1))
fi

echo
if [ "$fails" -eq 0 ]; then
	echo "install-browser contract holds"
	exit 0
fi
echo "$fails contract failure(s)"
exit 1
