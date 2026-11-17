#!/usr/bin/env bash
# Surfaces tests that RUN the code but do not ASSERT on it, by mutating the
# production code of the packages a diff touches and checking that some test
# fails.
#
# Why this exists at all: coverage counts execution, not detection. A spike on a
# 15-line function with thin assertions reached 90% statement coverage while two
# hand-made mutants (n > 100 -> n >= 100, n == 0 -> n != 0) both survived. The
# suite ran every line and asserted on almost none of them. `go test -cover`
# cannot see that gap; a surviving mutant is what makes it visible.
#
# The honest signal is EFFICACY: of the mutants a test actually reached, what
# fraction did a test kill. Assertion gaps are the mutants that LIVED.
#
# Do not treat one efficacy number as a fixed property of a package. The same
# target on the same machine gave 82.61%, 93.02%, 96.55% and 100.00% across
# repeated runs: gremlins re-runs the package's tests per mutant, and on a
# shared box its per-test timeout lands differently each time. Bad runs are not
# subtle, though — they report Killed: 0 with every mutant TIMED OUT (0%),
# never a quietly lower score. That shape is classified inconclusive below
# rather than reported as 0% efficacy, because "your tests assert nothing" and
# "the tool could not measure" must not look the same.
#
# Consequence: read the TREND across runs, not a single value, and gate only
# with visible headroom. Cost is ~75-150s per package, so the default caps at 3.
#
# Default is a report, not a gate: it exits 0 even when mutants survive. A new
# threshold on a metric nobody has baselined fails the first PR that touches any
# package and teaches people to bypass it. Wire it as a gate (MUTATION_GATE=1)
# only once the current efficacy is recorded and the threshold sits below it.
#
# Usage:
#   bash bin/check-mutation-drift.sh                    # changed packages
#   PKGS="./internal/i18n" bash bin/check-mutation-drift.sh
#   MUTATION_GATE=1 MUTATION_MIN_EFFICACY=80 bash bin/check-mutation-drift.sh
#   MUTATION_MAX_PACKAGES=2 bash bin/check-mutation-drift.sh   # bound the cost
#
# Output: one line per package (efficacy, killed/lived, elapsed) plus a footer
# naming the survivors' mutators. Nothing is written to the working tree: each
# mutant is applied and reverted by gremlins itself.

set -euo pipefail

project_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$project_dir"

# Bound the run. Mutation testing is quadratic-ish (one test run per mutant), so
# an unbounded run over the 118 packages of this repo is not a gate, it is a
# coffee break. The cap is loud: it names what it skipped.
max_packages="${MUTATION_MAX_PACKAGES:-3}"
min_efficacy="${MUTATION_MIN_EFFICACY:-80}"
gate="${MUTATION_GATE:-0}"
fail_on_no_tests="${MUTATION_FAIL_ON_NO_TESTS:-1}"

# gremlins comes from `go tool` (pinned in go.mod), which is this repo's rule for
# gates — never `go install ...@latest`, which would make the measurement depend
# on whatever the runner resolved that day. GREMLINS_BIN overrides for a spike.
if [ -n "${GREMLINS_BIN:-}" ]; then
  gremlins=( "$GREMLINS_BIN" )
else
  gremlins=( go tool gremlins )
  if ! ( cd "$project_dir" && "${gremlins[@]}" --version ) >/dev/null 2>&1; then
    cat >&2 <<'MSG'
❌ `go tool gremlins` is unavailable.

gremlins is pinned in go.mod's tool block; if that was lost, restore it rather
than installing an unpinned binary:

  go get -tool github.com/go-gremlins/gremlins/cmd/gremlins

Nothing was measured, so this is a SKIP (exit 2), not a pass.
MSG
    exit 2
  fi
fi

# Scope: PKGS wins (explicit), otherwise the packages this change touches. The
# --with-dependents closure is what a TEST run needs — a mutant in a leaf
# package is killed by the tests of the package that imports it just as well.
if [ -n "${PKGS:-}" ]; then
  read -r -a packages <<<"$PKGS"
else
  # `while read` rather than mapfile: macOS ships bash 3.2, which has no mapfile,
  # and this script must run on a developer's Mac, not only on CI.
  packages=()
  while IFS= read -r line; do
    [ -n "$line" ] && packages+=("$line")
  done < <(BASE="${BASE:-origin/master}" bash scripts/changed-packages.sh --with-dependents 2>/dev/null || true)
fi

if [ "${#packages[@]}" -eq 0 ]; then
  echo "✓ no Go package changed — nothing to mutate"
  exit 0
fi

# Disk guard. gremlins does not run mutants in place: for each worker it COPIES
# THE WHOLE MODULE (filepath.Walk over mod.Root) into a temp dir, .git and
# node_modules included, then deletes it on exit. On this repo that is ~450 MB
# per run; a crashed or interrupted run leaves the copy behind (three of ours
# had accumulated 12.7 GB). When the copy fails, gremlins does not report an
# error — it panics with "error, this is temporary" from
# engine.(*mutantExecutor).Start, and a full disk also corrupts the efficacy
# numbers, which is how an earlier version of this script reported a healthy
# package as 0%.
#
# So refuse up front, and sweep what earlier runs leaked.
repo_bytes=$(du -sk "$project_dir" 2>/dev/null | awk '{print $1}')
avail_kb=$(df -Pk "$project_dir" 2>/dev/null | awk 'NR==2 {print $4}')
need_kb=$(( repo_bytes * 2 ))
if [ -n "$avail_kb" ] && [ "$avail_kb" -lt "$need_kb" ]; then
  cat >&2 <<MSG
❌ not enough free disk to mutate safely.

gremlins copies the whole module (~$(( repo_bytes / 1024 )) MB) into a temp dir
per run, and needs headroom to also build. Available: $(( avail_kb / 1024 )) MB.

Free space first (make clean, docker system prune, or remove stale
\${TMPDIR}/gremlins-* leftovers from an interrupted run). Nothing was measured,
so this is a SKIP (exit 2), not a pass.
MSG
  exit 2
fi

# Sweep leftovers from runs that died before their deferred cleanup.
if compgen -G "${TMPDIR:-/tmp}/gremlins-*" >/dev/null 2>&1; then
  stale_kb=$(du -sk "${TMPDIR:-/tmp}"/gremlins-* 2>/dev/null | awk '{s+=$1} END {print s+0}')
  if [ "${stale_kb:-0}" -gt 0 ]; then
    echo "· removing $(( stale_kb / 1024 )) MB of gremlins tempdirs leaked by an interrupted run"
    rm -rf "${TMPDIR:-/tmp}"/gremlins-* 2>/dev/null || true
  fi
fi

# Only packages that HAVE tests can kill a mutant. Saying so out loud matters:
# a package with no test file reports efficacy 0 for a reason that is not the
# same as a package whose assertions are thin.
runnable=()
skipped_no_tests=()
for pkg in "${packages[@]}"; do
  [ -d "$pkg" ] || continue
  if compgen -G "$pkg/*_test.go" >/dev/null 2>&1; then
    runnable+=("$pkg")
  else
    skipped_no_tests+=("$pkg")
  fi
done

if [ "${#skipped_no_tests[@]}" -gt 0 ]; then
  echo "· no test file, not mutated: ${skipped_no_tests[*]}"
fi

if [ "${#runnable[@]}" -eq 0 ]; then
  if [ "$fail_on_no_tests" = "1" ]; then
    echo "❌ every changed package is untested — nothing could kill a mutant"
    echo "   (MUTATION_FAIL_ON_NO_TESTS=0 to downgrade this to a warning)"
    exit 1
  fi
  echo "⚠️  every changed package is untested (allowed by MUTATION_FAIL_ON_NO_TESTS=0)"
  exit 0
fi

if [ "${#runnable[@]}" -gt "$max_packages" ]; then
  echo "· ${#runnable[@]} packages changed, mutating the first $max_packages (MUTATION_MAX_PACKAGES)"
  echo "  skipped: ${runnable[*]:$max_packages}"
  runnable=("${runnable[@]:0:$max_packages}")
fi

# No flags, no config file — deliberately.
#
# `--config <file>` REPLACES the whole configuration rather than merging into it
# on this build (gremlins dev, darwin/arm64), and it zeroes the per-test timeout:
# a config file containing only `test-cpu: 1`, only `workers: 1`, all three
# keys, or even an EMPTY file all produce "Timed out: 4" on a suite whose tests
# finish in milliseconds — reported as efficacy 0.00%. The same values passed as
# --workers / --test-cpu / --timeout-coefficient flags fail identically. With no
# flags at all the same target reports the truth: 4 LIVED, 0.00% for a thin
# suite, and 82.61% for internal/i18n.
#
# So the defaults are what is correct here. They are also what this box wants:
# gremlins runs one worker by default, and this host is co-tenanted with 8 GB
# (AGENTS.md).

overall_failed=0
inconclusive=0
report=""
survivor_lines=""

for pkg in "${runnable[@]}"; do
  printf '→ %s … ' "$pkg"
  started=$(date +%s)
  out="$("${gremlins[@]}" unleash "$pkg" 2>&1 || true)"
  elapsed=$(($(date +%s) - started))

  efficacy=$(printf '%s\n' "$out" | sed -n 's/.*Test efficacy: *\([0-9.]*\)%.*/\1/p' | tail -1)
  killed=$(printf '%s\n' "$out" | sed -n 's/.*Killed: *\([0-9]*\).*/\1/p' | tail -1)
  lived=$(printf '%s\n' "$out" | sed -n 's/.*Lived: *\([0-9]*\).*/\1/p' | tail -1)

  if [ -z "$efficacy" ]; then
    echo "no report (${elapsed}s)"
    report+="$pkg | unknown | gremlins produced no parseable summary"$'\n'
    overall_failed=1
    continue
  fi

  killed="${killed:-0}"
  lived="${lived:-0}"

  # "All timed out" is the tool failing to measure, not a verdict on the tests.
  # Reported as-is it looks identical to a genuinely weak suite (efficacy 0%),
  # which is the one reading that would send someone to rewrite good tests or,
  # worse, teach them to ignore this check. gremlins does this when its
  # per-test timeout is miscalibrated (seen on a machine whose package tests
  # finish in 0.36s while every mutant reports TIMED OUT), so treat it as
  # inconclusive and say so.
  timed_out=$(printf '%s\n' "$out" | sed -n 's/.*Timed out: *\([0-9]*\).*/\1/p' | tail -1)
  timed_out="${timed_out:-0}"
  if [ "$killed" = "0" ] && [ "$lived" = "0" ] && [ "$timed_out" -gt 0 ]; then
    echo "inconclusive: ${timed_out} mutants timed out, 0 killed/lived (${elapsed}s)"
    report+="$pkg | inconclusive | ${timed_out} timed out, none killed or lived"$'\n'
    inconclusive=1
    continue
  fi

  echo "efficacy ${efficacy}% (killed ${killed}, lived ${lived}, ${elapsed}s)"

  # The surviving mutators ARE the finding: they name the assertions that are
  # missing, which a coverage number never does. gremlins prints one line per
  # mutant as `<STATUS> <MUTATOR> at <file>:<line>`, so the mutator is the
  # second field of a LIVED row. Counting by class (not by site) keeps the
  # footer readable when one missing assertion produces many survivors.
  survivors=$(printf '%s\n' "$out" | awk '$1 == "LIVED" { print $2 }' | sort | uniq -c | sort -rn | awk '{ printf "%s x%d, ", $2, $1 }' | sed 's/, $//')
  report+="$pkg | ${efficacy}% | killed ${killed} lived ${lived} | ${survivors:-none}"$'\n'
  # Collected here, inside the loop: `out` is rebound per package, so reading it
  # after the loop would name only the last package's survivors.
  survivor_lines+=$(printf '%s\n' "$out" | awk -v p="$pkg" '$1 == "LIVED" { printf "    %s %s at %s\n", p, $2, $4 }')

  if [ "$gate" = "1" ]; then
    # Integer-safe compare: bash cannot compare floats, so drop the decimal.
    eff_int=${efficacy%%.*}
    if [ -n "$eff_int" ] && [ "$eff_int" -lt "$min_efficacy" ]; then
      overall_failed=1
    fi
  fi
done

echo
echo "package | efficacy | outcome | surviving mutators"
printf '%s' "$report" | sed 's/^/  /'

# The surviving sites, not just their classes: "CONDITIONALS_NEGATION x3" says a
# kind of assertion is missing, "translator.go:124" says which one to write.
if [ -n "$survivor_lines" ]; then
  echo
  echo "  survivors (add an assertion that fails on each):"
  printf '%s' "$survivor_lines"
fi

if [ "$inconclusive" -eq 1 ] && [ "$gate" != "1" ]; then
  echo
  echo "⚠️  inconclusive: gremlins timed out every mutant on at least one package."
  echo "   That is a measurement failure, not weak tests — do not read it as 0% efficacy."
fi

if [ "$gate" = "1" ] && [ "$overall_failed" -ne 0 ]; then
  echo
  echo "❌ mutation efficacy below ${min_efficacy}% — tests reach the code without asserting on it"
  exit 1
fi

if [ "$gate" != "1" ]; then
  echo
  echo "✓ report only (MUTATION_GATE=1 to enforce, MUTATION_MIN_EFFICACY=${min_efficacy})"
fi
exit 0
