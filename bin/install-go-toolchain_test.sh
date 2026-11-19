#!/usr/bin/env bash
# Contract test for bin/install-go-toolchain.sh.
#
# The script has two jobs: get a trustworthy tarball onto the box, and never
# let a broken one reach /usr/local/go. The behaviour worth pinning is the
# CACHE handshake, because it is the part that can silently install a corrupt
# toolchain:
#
#   * a cached tarball must be used WITHOUT touching the network (that is the
#     only reason the cache exists — the download is the slow, flaky part);
#   * a corrupt cached tarball must NOT be installed; it is discarded and
#     re-fetched, so a truncated cache entry costs a download, not a gate;
#   * a cache miss must populate the cache, but only with a file that passes
#     the integrity check;
#   * a download that fails three times must take the job down — there is no
#     fallback to an ambient Go, which is the failure bin/check-ci-deps.sh
#     exists to prevent.
#
# curl, sudo and tar are stubs, so this runs in a second with no network, no
# root, and no real toolchain.
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
install="${here}/install-go-toolchain.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

stub_bin="$tmp/bin"
mkdir -p "$stub_bin"

# A tarball is real enough if gzip -t accepts it; the stub payload is one.
payload() { printf 'not-really-a-tarball-%s\n' "$1" | gzip -c; }

cat >"$stub_bin/curl" <<'STUB'
#!/usr/bin/env bash
# Stub downloader: writes a valid gzip body, or fails, and records the call.
printf 'curl %s\n' "$*" >>"$STUB_LOG"
[ "${CURL_FAIL:-0}" = "1" ] && exit 92
out=""
while [ $# -gt 0 ]; do
	case "$1" in
	-o) out="$2"; shift 2 ;;
	*) shift ;;
	esac
done
[ -n "$out" ] || exit 2
printf 'downloaded-body\n' | gzip -c >"$out"
exit 0
STUB

cat >"$stub_bin/sudo" <<'STUB'
#!/usr/bin/env bash
# Stub sudo: run the command. The tar/rm it wraps are stubbed below.
exec "$@"
STUB

cat >"$stub_bin/tar" <<'STUB'
#!/usr/bin/env bash
# Stub tar: prove the archive is readable, then lay down what a real
# extraction would (the `go` binary), so the caller's version check has
# something to run.
printf 'tar %s\n' "$*" >>"$STUB_LOG"
dest=""
prev=""
for a in "$@"; do
	case "$a" in
	*.tar.gz|*.tgz) gzip -t "$a" || exit 2 ;;
	esac
	[ "$prev" = "-C" ] && dest="$a"
	prev="$a"
done
[ -n "$dest" ] || exit 2
mkdir -p "$dest/go/bin"
printf '#!/usr/bin/env bash\necho "go version go1.99.0 linux/amd64"\n' >"$dest/go/bin/go"
chmod +x "$dest/go/bin/go"
exit 0
STUB

chmod +x "$stub_bin/curl" "$stub_bin/sudo" "$stub_bin/tar"

# The script installs into GO_INSTALL_PREFIX and then prints `<prefix>/go
# version`; a temp prefix keeps the test off the real /usr/local, and the tar
# stub above lays down the binary that line runs.
prefix="$tmp/prefix"

fails=0

run_case() { # run_case <description> <expect-exit> <expect-substring> [env...]
	local desc="$1" want_exit="$2" want_log="$3"
	shift 3
	local log="$tmp/calls.log"
	: >"$log"
	local out
	# The GO_TARBALL_CACHE every case runs with: the scenarios that are not
	# about the cache set it empty on purpose, so they stay cache-free.
	local out
	out="$(env PATH="$stub_bin:$PATH" STUB_LOG="$log" GO_VERSION=1.99.0 \
		GO_TARBALL_CACHE="${GO_TARBALL_CACHE:-}" \
		GO_INSTALL_PREFIX="$prefix" "$@" bash "$install" 2>&1)"
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
		printf 'FAIL  %s\n        want exit %s and output containing %q, got exit %s\n' \
			"$desc" "$want_exit" "$want_log" "$got"
		printf '%s\n' "$out" | sed 's/^/        | /'
		fails=$((fails + 1))
	fi
	printf '%s' "$out" >"$tmp/last-output"
	cp "$log" "$tmp/last-calls"
}

cache="$tmp/cache"
name="go1.99.0.linux-amd64.tar.gz"

echo "--- no cache configured behaves as it always did ---"
run_case "download, extract, report a version" 0 "go version go1.99.0" \
	GO_TARBALL_CACHE=
grep -q "^curl " "$tmp/last-calls" || {
	echo "FAIL  the tarball was never downloaded"; fails=$((fails + 1))
}

echo
echo "--- a cached tarball is used and the network is not touched ---"
mkdir -p "$cache"
payload cached >"$cache/$name"
run_case "cache hit skips the download" 0 "toolchain tarball from cache" \
	GO_TARBALL_CACHE="$cache"
if grep -q "^curl " "$tmp/last-calls"; then
	echo "FAIL  a cache hit still went to the network"; fails=$((fails + 1))
fi

echo
echo "--- a corrupt cached tarball must not reach the toolchain ---"
printf 'truncated' >"$cache/$name"
run_case "cache hit that fails the integrity check" 0 "discarding it" \
	GO_TARBALL_CACHE="$cache"
grep -q "^curl " "$tmp/last-calls" || {
	echo "FAIL  a corrupt cache entry did not trigger a re-fetch"; fails=$((fails + 1))
}
gzip -t "$cache/$name" 2>/dev/null || {
	echo "FAIL  the corrupt entry was left in place for the next job"; fails=$((fails + 1))
}

echo
echo "--- a miss populates the cache with a verified copy ---"
rm -f "$cache/$name"
run_case "cache miss stores what it downloaded" 0 "go version go1.99.0" \
	GO_TARBALL_CACHE="$cache"
gzip -t "$cache/$name" 2>/dev/null || {
	echo "FAIL  a cache miss did not leave a usable entry behind"; fails=$((fails + 1))
}

echo
echo "--- a download that never succeeds must take the job down ---"
run_case "the tarball is unavailable" 1 "toolchain download failed after 3 attempts" \
	GO_TARBALL_CACHE= CURL_FAIL=1
if grep -q "^tar " "$tmp/last-calls"; then
	echo "FAIL  tar ran on a toolchain that was never downloaded"; fails=$((fails + 1))
fi

echo
if [ "$fails" -eq 0 ]; then
	echo "install-go-toolchain contract holds"
	exit 0
fi
echo "$fails contract failure(s)"
exit 1
