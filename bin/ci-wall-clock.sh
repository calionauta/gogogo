#!/usr/bin/env bash
# ci-wall-clock — measure where a CI run actually spends its time.
#
# Why this exists. Every optimization in this repo that survived review was
# argued from a measurement, and every one that did not was argued from a
# guess. Two guesses from the 2026-11-19 session are recorded in
# docs/local-ci.md because they cost real time:
#
#   * "the browser job is slow because the Chromium cache got evicted" — both
#     runs were a cache HIT; the 102s difference was `apt-get install-deps`.
#   * "the `checks` job is slow because of lint" — Lint is 30s of 164s.
#
# A cost table goes stale silently: the installer fixture that shrank from 360s
# to 6s made the table wrong by a factor of 60, and nobody noticed until the
# numbers were re-taken. This script is how they get re-taken.
#
# Usage:
#   bin/ci-wall-clock.sh                 # the latest ci.yml run
#   bin/ci-wall-clock.sh <run-id>        # a specific run
#   bin/ci-wall-clock.sh <run-id> --steps  # also break down every job's steps
#
# Needs gh (authenticated) and a network. It only reads; it never dispatches.
set -uo pipefail

RUN=""
STEPS=0
for arg in "$@"; do
	case "$arg" in
	--steps) STEPS=1 ;;
	-h | --help)
		sed -n '2,24p' "$0" | sed 's/^# \{0,1\}//'
		exit 0
		;;
	'' | *[!0-9]*)
		echo "unknown argument: $arg" >&2
		exit 2
		;;
	*) RUN="$arg" ;;
	esac
done

if ! command -v gh >/dev/null 2>&1; then
	echo "ci-wall-clock: unavailable (gh not installed)"
	exit 0
fi
if ! gh auth status >/dev/null 2>&1; then
	echo "ci-wall-clock: unavailable (gh not authenticated)"
	exit 0
fi

if [ -z "$RUN" ]; then
	RUN="$(gh run list --workflow=ci.yml --limit=1 --json databaseId \
		--jq '.[0].databaseId' 2>/dev/null)"
	if [ -z "$RUN" ]; then
		echo "ci-wall-clock: unavailable (no ci.yml run found)"
		exit 0
	fi
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

if ! gh run view "$RUN" --json databaseId,jobs,conclusion,event,headBranch \
	>"$tmp/run.json" 2>/dev/null; then
	echo "ci-wall-clock: unavailable (gh run view $RUN failed)"
	exit 0
fi

python3 - "$tmp/run.json" "$STEPS" <<'PY'
import datetime
import json
import sys


def ts(value):
    return datetime.datetime.fromisoformat(value.replace("Z", "+00:00"))


data = json.load(open(sys.argv[1]))
show_steps = sys.argv[2] == "1"

run = data
event = run.get("event") or "?"
branch = run.get("headBranch") or "?"
conclusion = run.get("conclusion") or "in progress"

jobs = [j for j in data.get("jobs", []) if j.get("startedAt") and j.get("completedAt")]
if not jobs:
    print("ci-wall-clock: no completed jobs in this run")
    raise SystemExit(0)

origin = min(ts(j["startedAt"]) for j in jobs)


def span(job):
    start = (ts(job["startedAt"]) - origin).total_seconds()
    end = (ts(job["completedAt"]) - origin).total_seconds()
    return start, end


print(f"ci run {run.get('databaseId') or '?'}")
print(f"  event={event} branch={branch} conclusion={conclusion}")
print()

rows = []
for job in jobs:
    start, end = span(job)
    rows.append((end - start, start, end, job["name"]))

print("jobs, longest first (seconds relative to the first job to start):")
for duration, start, end, name in sorted(rows, reverse=True):
    print(f"  {duration:6.0f}s  start={start:6.0f} end={end:6.0f}  {name}")

wall = max(end for _, _, end, _ in rows)
print()
print(f"wall clock: {wall:.0f}s")
# The `ci` aggregator ends last by a few seconds and only waits on the other
# jobs, so it is never the thing to shorten. Rank by end time among jobs that
# actually did work.
substantive = [(d, s, e, n) for d, s, e, n in rows if d >= 10]
if substantive:
    last_end = max(e for _, _, e, _ in substantive)
    critical = [n for _, _, e, n in sorted(substantive) if e >= last_end - 1]
    print(f"critical path: {' + '.join(critical)} (ends at {last_end:.0f}s)")
    print("  (the run is only as short as the last job that did work;")
    print("   shortening any other job cannot move this number)")
    aggregator = [(n, e) for _, _, e, n in rows if n not in critical and e > last_end]
    for name, end in aggregator:
        print(f"  {name} ends {end - last_end:.0f}s later, but only aggregates")

if not show_steps:
    print()
    print("pass --steps to break down each job's steps")
    raise SystemExit(0)

for duration, _, _, name in sorted(rows, reverse=True):
    job = next(j for j in jobs if j["name"] == name)
    steps = [
        ((ts(s["completedAt"]) - ts(s["startedAt"])).total_seconds(), s["name"])
        for s in job.get("steps", [])
        if s.get("startedAt") and s.get("completedAt")
    ]
    if not steps:
        continue
    total = sum(d for d, _ in steps)
    print()
    print(f"== {name}: {duration:.0f}s of wall, {total:.0f}s of steps "
          f"({len(steps)} steps)")
    if duration > 0 and total / duration > 0.9:
        print("   steps are essentially serial: the job's wall IS their sum,")
        print("   so only moving work off this job shortens it")
    for step_duration, step_name in sorted(steps, reverse=True)[:8]:
        print(f"   {step_duration:6.1f}s  {step_name}")
PY
