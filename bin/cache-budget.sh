#!/usr/bin/env bash
# cache-budget — measure what the GitHub Actions cache is actually holding.
#
# Why this exists. GitHub caps a repository at 10 GB of Actions cache and
# evicts repo-wide by last access, and the repo sat at 11.0 GB without anyone
# noticing. Every measurement that found it was taken by hand with `gh api`,
# and the numbers moved between readings: the first pass blamed eviction, the
# second found 8.0 GB held by a single pull-request merge ref. A cost that
# nobody re-measures rots silently, which is the same argument
# bin/ci-wall-clock.sh makes for run time and bin/flake-budget.sh makes for
# retries. This is the third one.
#
# It reports; it does not delete. Eviction is GitHub's, entries are immutable,
# and a script that pruned by key shape would eventually delete a generation
# some branch is still being served. The one number that should make you open
# the workflow is the total against the cap.
#
# Usage:
#   bin/cache-budget.sh                  # repo inferred from the git remote
#   bin/cache-budget.sh owner/repo       # another repository
#   bin/cache-budget.sh --cap 20         # a higher cap (GB); the default is 10
#   bin/cache-budget.sh --json           # machine-readable
#   bin/cache-budget.sh --from-fixture f # read `gh api` JSON from a file
#
# The fixture mode exists so bin/cache-budget_test.sh can pin the arithmetic
# without a network or an authenticated gh, and so a real listing can be
# archived and re-read after the cache has moved on.
#
# Needs gh (authenticated) and a network. Prints "unavailable" instead of
# failing when either is missing, so a local run never breaks on this.
set -uo pipefail

CAP_GB=10
REPO=""
JSON=0
FIXTURE=""
while [ $# -gt 0 ]; do
	case "$1" in
	--cap) CAP_GB="${2:-10}"; shift 2 ;;
	--json) JSON=1; shift ;;
	--from-fixture) FIXTURE="${2:-}"; shift 2 ;;
	-h | --help)
		sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'
		exit 0
		;;
	-*) echo "unknown argument: $1" >&2; exit 2 ;;
	*) REPO="$1"; shift ;;
	esac
done

python3 - "$CAP_GB" "$REPO" "$JSON" "$FIXTURE" <<'PY'
import collections
import json
import os
import subprocess
import sys

cap_gb = float(sys.argv[1])
repo, as_json, fixture = sys.argv[2], sys.argv[3] == "1", sys.argv[4]


def unavailable(reason):
    print(f"cache-budget: unavailable ({reason})")
    raise SystemExit(0)


def fetch_lines():
    """Return the gh api listing as newline-delimited JSON objects."""
    if fixture:
        raw = open(fixture).read()
        # Accept either the paginated shape ({actions_caches: [...]}) or one
        # object per line, which is what --jq '.actions_caches[]' produces.
        try:
            doc = json.loads(raw)
        except json.JSONDecodeError:
            return [json.loads(line) for line in raw.splitlines() if line.strip()]
        key = "actions_caches" if isinstance(doc, dict) else None
        return doc[key] if key else (doc if isinstance(doc, list) else [doc])

    if subprocess.run(["sh", "-c", "command -v gh"], capture_output=True).returncode != 0:
        unavailable("gh not installed")
    # `gh auth status` exits non-zero when GH_TOKEN is set to a token it cannot
    # reach the API with — and in CI that is the ONLY credential there is, so
    # this check reported "not authenticated" on the one environment it runs in.
    # Presence of a token is the precondition; the api call below is the real
    # test, and it reports its own failure.
    if not os.environ.get("GH_TOKEN") and not os.environ.get("GITHUB_TOKEN"):
        if subprocess.run(["gh", "auth", "status"], capture_output=True).returncode != 0:
            unavailable("gh not authenticated")

    target = repo
    if not target:
        r = subprocess.run(
            ["gh", "repo", "view", "--json", "nameWithOwner", "--jq", ".nameWithOwner"],
            capture_output=True, text=True,
        )
        if r.returncode != 0:
            unavailable("could not resolve the repository (pass owner/repo)")
        target = r.stdout.strip()

    r = subprocess.run(
        ["gh", "api", f"repos/{target}/actions/caches", "--paginate",
         "--jq", ".actions_caches[]"],
        capture_output=True, text=True,
    )
    if r.returncode != 0:
        unavailable(f"gh api failed for {target}")
    return [json.loads(line) for line in r.stdout.splitlines() if line.strip()]


entries = []
for e in fetch_lines():
    entries.append({
        "id": e.get("id"),
        "key": e.get("key", ""),
        "ref": e.get("ref", ""),
        "mb": (e.get("size_in_bytes") or 0) / 1048576.0,
        "created": e.get("created_at") or e.get("createdAt") or "",
        "created_at": e.get("created_at") or e.get("createdAt") or "",
    })

if not entries:
    print("cache-budget: no cache entries")
    raise SystemExit(0)


def shape(key):
    """The part of a key that does not change per generation."""
    # Everything up to the trailing content hash: `go-build-Linux-checks-0-<sha>`
    # and `go-mod-dl-Linux-<sha>` both end in one 64-char hex blob.
    head, _, tail = key.rpartition("-")
    if len(tail) >= 32 and all(c in "0123456789abcdef" for c in tail):
        return head
    return key


total_mb = sum(e["mb"] for e in entries)

by_ref = collections.Counter()
for e in entries:
    by_ref[e["ref"]] += e["mb"]

# Duplicate generations: same (ref, shape) with more than one entry. This is
# the cost of a prefix restore-key across a dependency bump, not a bug, but it
# is the first thing to look at when the total climbs.
gens = collections.defaultdict(list)
entries_by_shape = collections.defaultdict(list)
for e in entries:
    k = (e["ref"], shape(e["key"]))
    gens[k].append(e["mb"])
    entries_by_shape[k].append(e)
dupes = {k: v for k, v in gens.items() if len(v) > 1}
waste_mb = sum(sum(v) - max(v) for v in dupes.values())


def _newest_key(ref, s):
    """The key a save would have written last — the one worth keeping.

    Ordered by creation, not access: a CI cache is written once and read many
    times, so the newest CREATED entry is the newest code. Falls back to the
    largest when timestamps are missing (the fixture shape).
    """
    group = entries_by_shape.get((ref, s), [])
    if not group:
        return None
    stamped = [e for e in group if e.get("created_at")]
    if stamped:
        return max(stamped, key=lambda e: (e["created_at"], e["mb"]))["key"]
    return max(group, key=lambda e: e["mb"])["key"]

# Entries on a pull-request merge ref. Before the restore/save split these were
# the dominant cost: a PR kept its own complete copy of every key shape.
pr_mb = sum(mb for ref, mb in by_ref.items() if ref.startswith("refs/pull/"))

if as_json:
    print(json.dumps({
        "total_mb": round(total_mb),
        "total_gb": round(total_mb / 1024, 2),
        "cap_gb": cap_gb,
        "headroom_gb": round(cap_gb - total_mb / 1024, 2),
        "entries": len(entries),
        "by_ref": {k: round(v) for k, v in by_ref.items()},
        "pull_request_mb": round(pr_mb),
        "duplicate_generation_mb": round(waste_mb),
    }, indent=2))
    raise SystemExit(0)

print(f"cache budget — {len(entries)} entries, {total_mb / 1024:.2f} GB "
      f"of a {cap_gb:.0f} GB cap")
headroom = cap_gb - total_mb / 1024
status = "OK" if headroom >= 0 else "OVER THE CAP"
print(f"  headroom: {headroom:+.2f} GB  ({status})")
print()
print("by ref:")
for ref, mb in by_ref.most_common():
    share = 100.0 * mb / total_mb
    print(f"  {mb / 1024:6.2f} GB  {share:5.1f}%  {ref}")

print()
print("by key shape:")
for (ref, s), sizes in sorted(gens.items(), key=lambda kv: -sum(kv[1]))[:8]:
    marker = f"  ({len(sizes)} generations)" if len(sizes) > 1 else ""
    print(f"  {sum(sizes):7.0f} MB  {s}{marker}")

if pr_mb:
    print()
    print(f"pull-request entries hold {pr_mb / 1024:.2f} GB. Since the restore/save")
    print("split only the default branch writes, so this figure should fall as")
    print("older PR refs age out (GitHub evicts by last access, ~7 days).")
if waste_mb:
    print()
    print(f"duplicate generations hold {waste_mb / 1024:.2f} GB. That is the cost")
    print("of a prefix restore-key surviving a dependency bump, which is the")
    print("trade-off that keeps a go.sum change from meaning a cold cache. It is")
    print("expected; it is also the first thing to look at if the total climbs.")
if headroom < 0:
    print()
    print("OVER THE CAP: GitHub evicts least-recently-used entries repo-wide once")
    print("the cap is reached, which is what silently discarded master's cache")
    print("before the restore/save split. Read docs/local-ci.md before changing a")
    print("key shape, and prefer removing a contributor over shrinking a hit rate.")
    print()
    # Naming WHICH entries to delete is the difference between a report and a
    # chore. Only the OLDEST generation of each shape is a candidate: the newest
    # one is what the next run restores, and deleting it buys a cold cache. Ids
    # are printed shallowest-use-first so a human can copy one into
    # `gh api -X DELETE repos/{owner}/{repo}/actions/caches/<id>` and see the
    # total drop.
    print("Reclaimable now — the oldest generation of each shape, newest kept:")
    reclaim_total = 0
    for (ref, s), sizes in sorted(gens.items(), key=lambda kv: -max(kv[1])):
        if len(sizes) < 2:
            continue
        stale = [e for e in entries_by_shape.get((ref, s), []) if e["key"] != _newest_key(ref, s)]
        stale = stale[: len(sizes) - 1]
        if not stale:
            continue
        freed = sum(e["mb"] for e in stale)
        reclaim_total += freed
        print(f"  {freed / 1024:5.2f} GB  {s}")
        for e in stale:
            print(f"             id={e['id']}  {e['key'][:64]}")
    print()
    print(f"  reclaimable total: {reclaim_total / 1024:.2f} GB  "
          f"(would bring the repo to {(total_mb - reclaim_total) / 1024:.2f} GB)")
    print("  delete one with:")
    print("    gh api -X DELETE repos/<owner>/<repo>/actions/caches/<id>")
    raise SystemExit(1)
PY
