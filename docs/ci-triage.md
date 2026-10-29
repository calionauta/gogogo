# When CI fails on something variable

A red build has three possible causes, and the first job is to tell them apart.
Treating a flake as a bug wastes a day; treating a bug as a flake ships it. This
page is the decision procedure, and it exists because the honest answer to "what
do I do when it's flaky?" is not obvious — the section at the end is about that.

## The 60-second triage

1. **Re-run the failed jobs** (Actions → the run → *Re-run failed jobs*). One
   click, and it answers the only question that matters first: does the same
   commit fail again?
2. **Two runs, same failure** → it is a **bug** in the product or in the test's
   assumptions. Fix it. Do not quarantine; a deterministic failure is the gate
   working.
3. **Second run green** → it was **variable**. Go to "Flaky" below.

Do not reach for `--admin`, and do not re-run more than twice to "make it go
away". A third run turns a signal into a coin flip.

## Which gate failed

| Failing job | What it means | Where to look |
|---|---|---|
| `checks` | a deterministic structural gate (sizes, scope, registry, generated artifacts, lint) | the log names the file and the rule |
| `race shard 0` | the Go suite under `-race`, on **`internal/installer` alone** — the measured outlier that gets a shard to itself | the test name and the stack are in the log |
| `race shard 1` / `race shard 2` | the same suite, the long tail split in two | same; each shard prints `N of M packages` so you can see what it ran |
| `browser group 0` / `browser group 1` | one of five Playwright suites against the real binary, grouped by measured cost | `scripts/run-browser-suite.sh` prints which suite, and both attempts |
| `site` | the docs site builds and every published page loads at seven widths | the suite names the page and the width |
| `prod-image` / `gui` / `template` | packaging and downstream builds | the failing step |

The `race` split is by measured cost, not by count: `scripts/ci-shard-packages.sh`
keeps the cost table, and `internal/installer` earns a shard alone because its
units each copy the repo and fork a `go vet ./...`. A Go failure is therefore
most often in shard 0, and a shard that prints fewer packages than you expected
means the PR narrowed the scope (or the cost table needs a refresh — the command
is in the script's header).

A `browser` failure is the only one that can be variable in a way that is not a
product bug, which is why it is the only one with a retry.

## Flaky

A flake is a **test** that does not produce the same result on identical inputs.
In practice it is almost always a synchronization mistake, not randomness: the
test waited for something that only *usually* happens before what it asserts.

Diagnosis order — the first one is by far the most common:

1. **A wait on a proxy rather than the effect.** The test awaited signal A (a
   DOM node, a response, a cursor) and then sampled effect B (canvas pixels, a
   signal's value, a list's contents). A arriving does not order B. Fix: wait on
   B itself, or on a state the app sets *after* committing B.
   - Measured example: the whiteboard negative-drag assertion waited for the
     peer's cursor dot and then sampled canvas ink. Same source, `ink 97` one run
     and `ink 0` the next. Fixed by polling the ink (`waitForInk`).
2. **A fixed nap.** `await sleep(2000)` or `page.waitForTimeout(2000)` encodes a
   guess about how long something takes. `make check-naps` (in the Go suite)
   fails on a long nap without a written reason — see
   `internal/capabilities/nap_guard_test.go` for the two bands.
3. **State leaking between tests.** A package global written by one test, a
   shared fixture, a fixed port. Run the test alone (`go test ./pkg -run X
   -count=1`); if it passes alone and fails in the package, that is the shape.
4. **Resource starvation.** The suite is correct and the box is not: parallel
   load, a full disk, a saturated CPU. `make ci-local` refuses on this host by
   design, and `internal/hostgate` skips the heavy tests when constrained.

### What to do about a confirmed flake

In order, and stop at the first that applies:

- **Fix the wait.** This is almost always possible and always the right answer.
  A flake usually marks a real ordering assumption in the test that would bite a
  user too.
- **Delete the test** if it guards nothing that is not covered elsewhere. A test
  that cannot be made deterministic and guards nothing is worse than no test.
- **Quarantine it** — and only as a last resort, with a deadline. See below.

## Quarantine, and why it has an expiry

`internal/hostgate/quarantine.go` lets a known-flaky test stop gating the build
while keeping its reason, an issue link, and a **date** on record:

```go
func TestSomethingKnownFlaky(t *testing.T) {
    if !hostgate.Quarantine(t, "TestSomethingKnownFlaky") {
        return
    }
    // ... the real test
}
```

with a registry entry carrying `reason`, `issue` and `expires` (an absolute
date — a relative one would move every time someone touched it, which is how a
quarantine becomes permanent).

Behavior:

| State | What happens |
|---|---|
| not registered | runs normally (the helper is a no-op for every other test) |
| registered, before expiry | **skips**, loudly, naming reason + issue + date |
| registered, before expiry, `QUARANTINE_REPORT=1` | **runs** so the signal is still visible |
| registered, after expiry | **runs normally**, so it fails — the deadline is a reminder you cannot miss |

`TestQuarantineRegistryIsValid` (in the `checks` gate) fails the build on an
entry that is missing a reason, missing an issue, malformed, or **expired**. That
last one is the whole point: a quarantine without an enforced deadline is a
deletion that lies about itself — the test stays in the tree, stops running, and
nobody notices the coverage is gone.

Keep them short. Real evidence for that, not vibes: Google reported that when a
previously-stable test turned flaky and was traced to a change, it was a **real
production bug in roughly one case out of six** (a blog comment rather than a
paper, so directional). A quarantine is therefore a postponement with a
deadline, never a verdict.

**The registry is currently empty.** That is the correct state.

## Retries: what they are for, and what they are not

`scripts/run-browser-suite.sh` gives a failed browser suite exactly one retry,
and a pass on retry is reported as **FLAKY** — not as green:

- an `::warning` annotation on the run,
- a `⚠️` line on stderr,
- a `GITHUB_STEP_SUMMARY` section naming both logs and the retry's Playwright
  trace.

So a retried pass still exits 0 (it does not block the PR) but it cannot be
mistaken for a clean pass. The retry is *diagnostic instrumentation*: it
separates "the product is broken" from "the machine starved this run". It is not
a fix, and a suite that needs its retry regularly **is** the finding — that is
what the summary section is for.

Two runs, not more. A retry that keeps being spent is debt, and the alternative
to retrying forever is fixing the wait.

## Why 100% is not the target

This is the part that answers "if there is nothing to fix, what do I do?".

You do not eliminate flake; you bound it. The reasons are structural, not
laziness:

- **Flakiness is not a static property of the test's source.** The 2026
  reproduction study ([arXiv 2607.09345](https://arxiv.org/html/2607.09345))
  found that code-based detectors, once benchmark shortcuts are removed, do no
  better than a majority-class baseline — and that for 86 real flaky E2E tests
  mined from 450k CI runs, source + log could attribute a cause for only 42%.
  The rest needed execution evidence.
- **It is undecidable in general.** "This test always produces the same result"
  is a non-trivial semantic property of a program, so Rice's theorem applies to
  any static predictor.
- **The practitioners say the same, in their own words.** Meta: *"Our goal is
  not to assert that any test is 100 percent reliable, because that's not
  realistic."* Google (Micco): *"flaky tests are inevitable when the test
  condition reaches a certain complexity… If they are not flaky, they tend to
  never find bugs."*
- **The size distribution is the honest signal.** Google measured flake rates by
  test size: ~0.5% small, ~1.6% medium, **~14% large**. Browser E2E is the large
  end. A suite of this kind sitting at a low single-digit flake rate is working
  as designed, not broken.

What replaces "zero" as the target: a **flake rate** you watch (the retry
summary makes it visible), a **detection latency** you keep short (the retry
plus the trace), and a **quarantine SLA** (the enforced expiry). If a suite
flakes once a month and the retry labels it, nothing needs fixing. If it flakes
weekly, the retry is telling you which wait to correct.

**So, concretely, when CI goes red on something variable and there is nothing to
fix:** read the flake summary, confirm the retry labelled it, and move on. You
have already done the work — the retry exists precisely so a variable failure
does not need a human decision every time. The action item is only created when
the SAME suite appears in the flake summary repeatedly, and then the fix is the
wait, not the retry count.
