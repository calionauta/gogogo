# Recipe outcomes: the job, declared as two counts

Reviewed: 2026-11-12

This note records the decision behind `Declaration.Outcomes`. It follows
[Recipe metrics and panel spec](recipe-metrics-panel-2026-11.md), which declared
metrics as one `SELECT COUNT(*)` per entry and named its revisit trigger: *a
recipe needing a non-COUNT metric, or an outcome rather than a volume.* That
trigger is now reached — a voice demo whose value is "the order was right without
a repair" cannot be expressed as a count of rows.

## The question

Where does a recipe declare *what it is for*, and how is that measured without
adding a collection system?

Three shapes were on the table:

1. **An event stream per recipe** — the recipe declares events, emits them from
   its handlers, and the engine aggregates. Owned table, owner-scoped, readable.
2. **A core events service** — one table every recipe writes, with a retention
   policy, an on/off switch, and a query surface.
3. **A pair of counts over rows the recipe already stores.**

## Decision: the pair of counts

An outcome is `success / total`, both declared metrics:

```yaml
outcomes:
  - id: order_without_repair
    job: Receive the order you asked for without fixing a misread order
    beneficiary: user
    success:
      label: Orders without repair
      query: "SELECT COUNT(*) FROM ext_kiosk_orders WHERE owner = ? AND corrected = 0"
    total:
      label: Orders confirmed
      query: "SELECT COUNT(*) FROM ext_kiosk_orders WHERE owner = ?"
    counterSignals: [corrected]
    window: session
```

Why this and not an event stream:

- **One definition of a valid query.** `success` and `total` are `Metric`, so
  `validCount` (already the wall for dashboard counts) validates both halves.
  There is no second query language and no aggregation builder.
- **No new storage, so no new policy.** The denominator is rows the feature
  already owns; `corrected` is a column on the order, not telemetry. A retention
  policy, an opt-in toggle and a privacy review would all be about data that a
  demo does not collect.
- **Ownership stays attributable.** A core-wide events table is a capability
  every recipe inherits and no recipe can uninstall — the opposite of the
  boundary that makes `ext_<id>_` tables safe. A per-recipe events table repeats
  DDL and leaves rows orphaned after uninstall.
- **Honest about absence.** A recipe with no stored fact for its counter signal
  has no outcome, and the dashboard says "no data" rather than inventing one.

## What is validated, and where

`OutcomeProblems` is pure data in / strings out, like `MetricProblems`, and runs
in three places that must agree: boot (registration), `recipe check`, and the
catalog review.

1. `id` is snake_case and unique;
2. `job` is non-empty — a ratio without the sentence is a number nobody can
   disagree with;
3. `beneficiary` is `user` or `operator`, and the two are never one declaration;
4. `success` and `total` each pass the metric rules (`COUNT(*)`, one `?` when
   owner-scoped);
5. **both queries read a table the declaration lists.** This is the only
   enforceable part of "is it instrumented": a query over a table the recipe does
   not own proves the outcome is describing something else;
6. `target`, when present, is in `(0,1]`; absent is the default, because an
   invented target is a decision made on the operator's behalf;
7. `counterSignals` are closed tokens, unique;
8. `window` is `session`, `day` or `none`.

One rule needs the code, not just the declaration: a counter signal the recipe's
code never mentions (`OutcomeInstrumentationGaps`). `recipe check` grades it an
**error** — the token is the state the ratio is limited by, so a success count
nobody can move is a number nobody can read. The engine never applies it: an
older receipt has to keep running, so the gate sits in the check, not in boot.

## What the dashboard shows

`success/total · percentage`, and `—` when the denominator is zero. Never the
percentage alone: 1/1 and 100/100 are the same 100% and very different evidence.
No composite score, no single quality number — the counter signal sits beside the
success count, so a rise in corrections cannot be averaged away by a rise in
volume.

## Why not a Taguchi signal-to-noise ratio

S/N is a designed-experiment statistic: it is estimated by crossing control
factors with noise factors across replications, and the literature criticises it
precisely for collapsing mean and variance into one number (Box; the PerMIA work
of Leon–Shoemaker–Kacker), which loses information when the variance tracks the
mean. Ryan Singer's adaptation of the idea to product usage (2021) is a lens on
intent — "is this being used as intended?" — not a statistic computable from
session telemetry.

So: numerator, denominator, window and exclusions are explicit; a job sentence and
a counter signal are required; an S/N value is not. If a recipe ever has repeated
responses with declared control/noise factors and the volume to support it, that
is a new decision with data behind it.

## Authorship

The recipe-authoring skill is the procedure: ask which job the recipe performs
and for whom, propose **one** outcome plus **one** plausible counter signal, show
the YAML, and wait for the operator to confirm or edit before writing it.
Recommendations are marked as recommendations; `target: null` is the honest
default; nothing is instrumented that the operator did not ask for. With this
contract "collection" is just the row the feature already writes, so there is no
hidden telemetry to disable.

## Revisit triggers

- An outcome whose counter signal is not reducible to a column the recipe
  stores. Then the recipe owns a table for it — still not a core pipeline.
- Non-COUNT aggregates (percentiles, windowed sums) appearing in a real recipe.
- Comparing one outcome across recipes, which needs a shared key the pair-of-
  counts shape deliberately does not provide.

## Status

Shipped 2026-11-12: `Outcome`, `OutcomeProblems`,
`OutcomeInstrumentationGaps`, dashboard rendering, `recipe check` output, and
the `kiosk` demo declaring `order_without_repair`. Receipts without outcomes are
unchanged and score the same.
