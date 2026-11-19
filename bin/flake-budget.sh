#!/usr/bin/env bash
# flake-budget — measure how often a run had to retry to go green.
#
# Why this exists. The browser suites retry a failed test once and mark it
# flaky (docs/local-ci.md), and the race suite deliberately never retries.
# That policy is only honest if somebody measures the rate: a retry that
# silently turns red into green is how a chronically flaky test survives
# forever, because nobody ever sees the red. The 2026 practice is a stated
# BUDGET (e.g. "<=1% of master runs need a retry") rather than the aspiration
# of zero flakes, plus a per-test rule (quarantine at >=3 failures in 50 runs)
# so a fix has an owner and a deadline.
#
# This prints the numbers that budget is judged on. It does not gate: a
# threshold in CI would make the gate red for a reason that has nothing to do
# with the change under test, which is the failure mode the budget exists to
# prevent. Read it monthly, or when a run felt slow and retried.
#
# Usage:
#   bin/flake-budget.sh                  # last 30 CI runs on the default branch
#   bin/flake-budget.sh --limit 60       # a longer window
#   bin/flake-budget.sh --branch master  # another ref
#
# Needs gh (authenticated) and a network. Prints "unavailable" instead of
# failing when either is missing, so a local run never breaks on this.
set -uo pipefail

LIMIT=30
BRANCH="master"
while [ $# -gt 0 ]; do
	case "$1" in
	--limit) LIMIT="${2:-30}"; shift 2 ;;
	--branch) BRANCH="${2:-master}"; shift 2 ;;
	-h | --help)
		sed -n '2,25p' "$0" | sed 's/^# \{0,1\}//'
		exit 0
		;;
	*)
		echo "unknown argument: $1" >&2
		exit 2
		;;
	esac
done

if ! command -v gh >/dev/null 2>&1; then
	echo "flake-budget: unavailable (gh not installed)"
	exit 0
fi
if ! gh auth status >/dev/null 2>&1; then
	echo "flake-budget: unavailable (gh not authenticated)"
	exit 0
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# Job-level conclusions, oldest first, for the window.
if ! gh run list --workflow=ci.yml --branch="$BRANCH" --limit="$LIMIT" \
	--json databaseId,conclusion,createdAt >"$tmp/runs" 2>/dev/null; then
	echo "flake-budget: unavailable (gh run list failed)"
	exit 0
fi

total=$(python3 - "$tmp/runs" <<'PY'
import json,sys
runs=json.load(open(sys.argv[1]))
print(len([r for r in runs if r.get("conclusion")]))
PY
)
if [ "$total" -eq 0 ]; then
	echo "flake-budget: no completed runs in the window"
	exit 0
fi

# A run that needed a retry shows a FAILED job attempt alongside a successful
# final conclusion. `gh run view --json jobs` lists the jobs, not the attempts,
# so the signal used here is the job's conclusion against the run's: a job that
# ended `failure` or `cancelled` in an otherwise successful run means something
# ran more than once, or was cancelled and superseded. Both are counted, and
# the second is labelled separately so a real flake is not buried in
# cancellations (concurrency supersedes runs on every push).
python3 - "$tmp/runs" "$tmp/jobs.jsonl" <<'PY'
import json,subprocess,sys
runs=json.load(open(sys.argv[1]))
out=open(sys.argv[2],'w')
for r in runs:
    if not r.get("conclusion"):
        continue
    try:
        raw=subprocess.run(["gh","run","view",str(r["databaseId"]),"--json","jobs,conclusion"],
                           capture_output=True,text=True,timeout=60)
    except Exception:
        continue
    if raw.returncode!=0:
        continue
    try:
        d=json.loads(raw.stdout)
    except Exception:
        continue
    out.write(json.dumps({"run":{"id":str(r["databaseId"]),"conclusion":d.get("conclusion") or r.get("conclusion")},
                          "jobs":[{"name":j.get("name"),"conclusion":j.get("conclusion")} for j in d.get("jobs",[])]})+"\n")
out.close()
PY

python3 - "$tmp/jobs.jsonl" <<'PY'
import json,sys
runs=[json.loads(l) for l in open(sys.argv[1]) if l.strip()]
if not runs:
    print("flake-budget: unavailable (no job detail could be read)")
    raise SystemExit(0)
green=[r for r in runs if r["run"]["conclusion"]=="success"]
if not green:
    print("flake-budget: no successful runs in the window")
    raise SystemExit(0)

needed_retry=0
cancelled_only=0
per_job={}
for r in green:
    bad=[j for j in r["jobs"] if j["conclusion"] in ("failure","timed_out")]
    canc=[j for j in r["jobs"] if j["conclusion"]=="cancelled"]
    if bad:
        needed_retry+=1
        for j in bad:
            per_job[j["name"]]=per_job.get(j["name"],0)+1
    elif canc:
        cancelled_only+=1

n=len(green)
pct=100.0*needed_retry/n
print(f"flake budget — last {n} successful CI run(s) on the branch")
print(f"  runs that had a failing job before going green: {needed_retry} ({pct:.1f}%)")
print(f"  runs that only cancelled superseded jobs:       {cancelled_only}")
budget_ok = pct <= 1.0
print(f"  budget (<=1.0% of runs needing a retry):        {'OK' if budget_ok else 'OVER'}")
if per_job:
    print("  jobs that failed inside an otherwise-green run:")
    for name,count in sorted(per_job.items(), key=lambda kv:-kv[1]):
        print(f"    {count:3d}  {name}")
    print("\n  A job that shows here twice or more is the quarantine candidate:")
    print("  mark it known-flaky, keep running it, give it an owner and a date")
    print("  (docs/local-ci.md). One occurrence may be infrastructure — check the")
    print("  log before calling it flaky, and preserve the first failure's log.")
PY
