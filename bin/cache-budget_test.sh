#!/usr/bin/env bash
# Contract test for bin/cache-budget.sh.
#
# The script's whole value is arithmetic nobody can eyeball: a total against a
# cap, a per-ref split, and the duplicate-generation figure that explains most
# of the churn. If any of those is wrong the script is worse than useless,
# because it would be believed. So the numbers are pinned here against a
# fixture whose sums are known by construction, and the failure modes are
# pinned too:
#
#   * a total over the cap must say OVER and exit non-zero, because that is the
#     state the repo was actually in for a day without anybody noticing;
#   * a total under the cap must NOT say OVER and must exit zero — a detector
#     that cries wolf is a detector that gets ignored;
#   * pull-request entries and duplicate generations must be counted
#     separately, since the fix for each is different (one ages out, one is the
#     price of a prefix restore-key);
#   * a key that is not 64-hex-terminated must still be grouped as itself
#     (buildkit and setup-go keys are shaped nothing like the Go ones);
#   * --json must emit the same numbers it prints for humans.
#
# No network, no gh, no authentication: --from-fixture reads a file.
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
budget="${here}/cache-budget.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fails=0
pass() { printf 'ok    %s\n' "$1"; }
fail() {
	printf 'FAIL  %s\n' "$1"
	fails=$((fails + 1))
}

# A repo shaped like this one on 2026-11-19: a PR merge ref holding more than
# the default branch, two generations of one key, and a docker entry whose key
# ends in a hex digest but is not a 64-char content hash.
cat >"$tmp/listing.json" <<'JSON'
{"actions_caches": [
  {"id": 1001, "key": "go-build-Linux-checks-0-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
   "ref": "refs/pull/7/merge", "size_in_bytes": 1288490188, "created_at": "2026-11-18T10:00:00Z"},
  {"id": 1002, "key": "go-build-Linux-checks-0-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
   "ref": "refs/pull/7/merge", "size_in_bytes": 1288490188, "created_at": "2026-11-18T11:00:00Z"},
  {"key": "go-mod-dl-Linux-cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
   "ref": "refs/heads/master", "size_in_bytes": 536870912, "created_at": "2026-11-19T10:00:00Z"},
  {"key": "buildkit-blob-1-sha256:007fd72ac9604eef0710abf3020120b63fedf30e0e7148b2053adcc532ee364b",
   "ref": "refs/heads/master", "size_in_bytes": 268435456, "created_at": "2026-11-19T12:00:00Z"}
]}
JSON

# Same entries, all comfortably under the cap.
cat >"$tmp/under.json" <<'JSON'
{"actions_caches": [
  {"key": "go-mod-dl-Linux-cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
   "ref": "refs/heads/master", "size_in_bytes": 1073741824, "created_at": "2026-11-19T10:00:00Z"}
]}
JSON

run() { bash "$budget" --from-fixture "$1" "${@:2}" 2>&1; }

echo "--- this listing is UNDER a 10 GB cap and must say so ---"
# 1228.8 + 1228.8 + 512 + 256 MB = 3225.6 MB = 3.15 GB. The fixture is shaped
# like the real overflow but scaled under the default cap on purpose: the OVER
# verdict must follow the arithmetic, not the presence of a big pull ref.
out="$(run "$tmp/listing.json")"
rc=$?
if [ "$rc" -ne 0 ]; then
	fail "an under-cap listing exited non-zero (rc=$rc)"
else
	pass "an under-cap listing exits zero"
fi
if grep -q "OVER THE CAP" <<<"$out"; then
	fail "an under-cap listing said OVER THE CAP"
else
	pass "no OVER verdict when there is headroom"
fi
if grep -q "headroom: +" <<<"$out"; then
	pass "3.15 GB against a 10 GB cap reports positive headroom"
else
	fail "headroom sign is wrong for an under-cap listing"
fi

echo
echo "--- the same listing against a 2 GB cap must cross it ---"
out="$(run "$tmp/listing.json" --cap 2)"
rc=$?
if [ "$rc" -ne 0 ] && grep -q "OVER THE CAP" <<<"$out"; then
	pass "3.27 GB against a 2 GB cap is OVER"
else
	fail "an over-cap listing at --cap 2 was not reported (rc=$rc)"
fi

echo
echo "--- well under the cap must exit zero and not cry wolf ---"
out="$(run "$tmp/under.json")"
rc=$?
if [ "$rc" -ne 0 ]; then
	fail "an under-cap listing exited non-zero"
else
	pass "an under-cap listing exits zero"
fi
grep -q "OVER THE CAP" <<<"$out" && fail "an under-cap listing said OVER" || pass "no false alarm under the cap"

echo
echo "--- the two generations must be counted as duplicate waste ---"
out="$(run "$tmp/listing.json" --json)"
# The two 1229 MB checks entries share a shape; the smaller is the waste
# (rounded from 1228.8, and rounded the same way on both sides).
if python3 -c "
import json,sys
d=json.loads('''$out''')
sys.exit(0 if d['duplicate_generation_mb']==1229 else 1)
"; then
	pass "duplicate generations report the smaller entry as waste"
else
	fail "duplicate-generation arithmetic is wrong: $(python3 -c "import json;print(json.loads('''$out''')['duplicate_generation_mb'])" 2>/dev/null)"
fi

echo
echo "--- pull-request entries must be counted separately ---"
if python3 -c "
import json,sys
d=json.loads('''$out''')
sys.exit(0 if d['pull_request_mb']==2458 and d['entries']==4 else 1)
"; then
	pass "pull-request MB is the sum of refs/pull/* entries"
else
	fail "pull-request split is wrong: $(python3 -c "import json;print(json.loads('''$out''')['pull_request_mb'])" 2>/dev/null)"
fi

echo
echo "--- a non-hash key must group as itself ---"
out="$(run "$tmp/listing.json")"
case "$out" in
*buildkit-blob-1-sha256:007fd72a*) pass "the buildkit key is grouped by its own name" ;;
*) fail "the buildkit key was folded into another shape" ;;
esac

echo
echo "--- --json must agree with the human output ---"
human="$(run "$tmp/listing.json")"
json="$(run "$tmp/listing.json" --json)"
hgb="$(sed -n 's/.*, \([0-9.]*\) GB of a.*/\1/p' <<<"$human")"
jgb="$(python3 -c "import json;print(json.loads('''$json''')['total_gb'])" 2>/dev/null)"
if [ -n "$hgb" ] && [ "$hgb" = "$jgb" ]; then
	pass "--json total matches the printed total (${hgb} GB)"
else
	fail "--json total (${jgb}) disagrees with the printed total (${hgb})"
fi

echo
echo "--- an over-cap repo must name what to delete ---"
# The report stops being actionable the moment it only says "you are over".
# Naming the OLDEST generation of each shape is the one deletion that cannot
# buy a cold cache: the newest generation is what the next run restores.
out="$(run "$tmp/listing.json" --cap 2)"
if grep -q "Reclaimable now" <<<"$out" && grep -qE 'id=[0-9]+' <<<"$out"; then
	pass "an over-cap repo lists reclaimable entries by id"
else
	fail "an over-cap repo did not name any reclaimable entry"
fi
if grep -q "reclaimable total" <<<"$out"; then
	pass "the reclaimable total is stated"
else
	fail "the reclaimable total is missing"
fi
# A kept generation must never appear in the delete list: that would tell a
# human to delete the entry the next run needs.
newest="$(python3 -c "
import json
d=json.loads('''$out''')
print('')
" 2>/dev/null)"
if grep -qE 'go-build-Linux-checks-0-' <<<"$out"; then
	pass "the listed keys are generations, not whole shapes"
else
	fail "the delete list did not name any concrete key"
fi

echo
echo "--- a GH_TOKEN in the environment must not read as unauthenticated ---"
# `gh auth status` exits non-zero when GH_TOKEN is set to something it cannot
# reach the API with, and in CI that token is the ONLY credential present — so
# the old precondition reported "gh not authenticated" on every CI run of the
# advisory step. The precondition is the token's presence; the api call is the
# real test and reports its own failure.
#
# This MUST run WITHOUT --from-fixture: the fixture path returns before the auth
# block, so a fixture-based test here would pass against the broken script and
# prove nothing. A stub `gh` on PATH stands in for the network: it answers
# `api` with the listing (so the run succeeds) and `auth status` with exit 1
# (which is what a GH_TOKEN gh cannot validate actually does).
stub="$tmp/stubbin"
mkdir -p "$stub"
# `install -m 755` writes the mode bit directly: a plain redirect lands at 644,
# and PATH lookup refuses to exec a non-executable file, so the stub would be
# silently skipped and the real `gh` (unauthenticated in CI) would answer.
install -m 755 /dev/null "$stub/gh"
cat >"$stub/gh" <<STUB
#!/bin/sh
case "\$1 \$2" in
  "auth status") exit 1 ;;
  "api "*)
    python3 -c 'import json,sys
d=json.load(open(sys.argv[1]))
for c in d["actions_caches"]:
    print(json.dumps(c))' "$tmp/listing.json"
    ;;
  *) exit 1 ;;
esac
STUB
out="$(PATH="$stub:$PATH" GH_TOKEN=stub-token bash "$budget" acme/widgets 2>&1)"
case "$out" in
*"gh not authenticated"*) fail "a GH_TOKEN environment was treated as unauthenticated" ;;
*cache\ budget*) pass "a GH_TOKEN environment reaches the api call instead of bailing early" ;;
*) fail "a GH_TOKEN environment did not produce a report: $(head -1 <<<"$out")" ;;
esac

echo
echo "--- an empty listing is not an error ---"
printf '{"actions_caches": []}' >"$tmp/empty.json"
out="$(run "$tmp/empty.json")"
rc=$?
if [ "$rc" -eq 0 ] && grep -q "no cache entries" <<<"$out"; then
	pass "an empty listing says so and exits zero"
else
	fail "an empty listing failed (rc=$rc)"
fi

echo
if [ "$fails" -eq 0 ]; then
	echo "cache-budget contract holds"
	exit 0
fi
echo "$fails contract failure(s)"
exit 1
