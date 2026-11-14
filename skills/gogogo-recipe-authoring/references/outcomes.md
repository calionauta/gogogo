# Metrics — declaring what the recipe measures

Read this when a recipe has to say **whether it worked**, not just how much
happened. It is the full contract behind the `metrics` list; the engine enforces
it at boot, at install and at `recipe status`, so a declaration that disagrees
with the code never ships.

## One list, two kinds

Every reading is a `metric`, and `kind` says what it claims:

- **`activity`** counts. `Orders confirmed: SELECT COUNT(*) FROM
  ext_kiosk_orders WHERE owner = ?`. One number, no denominator, no claim. It is
  also the default: an entry with no `kind` is activity.
- **`outcome`** answers a job for a beneficiary, as **success over total**, plus
  the counterbalance readings that qualify the ratio. It renders as `2/2 · 100%`
  with its counterbalance numbers beneath.

One list because both answer "is it working" at different strengths. Two
parallel fields (`metrics:` and `outcomes:`) made the reader guess which one to
trust, and the dashboard had to merge them anyway.

An outcome is not an event stream, not a funnel and not a score. If you find
yourself needing history to compute it, the recipe is missing a **column**, not
a pipeline.

## Schema

```yaml
metrics:
  - label: Orders confirmed          # activity: no kind, no claim
    query: "SELECT COUNT(*) FROM ext_kiosk_orders WHERE owner = ?"

  - kind: outcome
    id: order_without_repair
    job: Receive the order you asked for without fixing a misread order
    beneficiary: user                # user | operator — never both
    success:
      label: Orders without repair
      query: "SELECT COUNT(*) FROM ext_kiosk_orders WHERE owner = ? AND corrected = 0"
    total:
      label: Orders confirmed
      query: "SELECT COUNT(*) FROM ext_kiosk_orders WHERE owner = ?"
    counterbalance:                  # real counts, not tokens
      - label: Orders corrected
        query: "SELECT COUNT(*) FROM ext_kiosk_orders WHERE owner = ? AND corrected = 1"
    window: session                  # session | day | none
    target: 0.9                      # optional; null unless the operator agreed
```

The same declaration in Go, in the recipe's `Declaration()`:

```go
Metrics: []recipe.Metric{
    {
        Label: "Orders confirmed",
        Query: "SELECT COUNT(*) FROM ext_kiosk_orders WHERE owner = ?",
    },
    {
        Kind:        recipe.MetricOutcome,
        ID:          "order_without_repair",
        Job:         "Receive the order you asked for without fixing a misread order",
        Beneficiary: "user",
        Success: &recipe.Count{
            Label: "Orders without repair",
            Query: "SELECT COUNT(*) FROM ext_kiosk_orders WHERE owner = ? AND corrected = 0",
        },
        Total: &recipe.Count{
            Label: "Orders confirmed",
            Query: "SELECT COUNT(*) FROM ext_kiosk_orders WHERE owner = ?",
        },
        Counterbalance: []recipe.Count{{
            Label: "Orders corrected",
            Query: "SELECT COUNT(*) FROM ext_kiosk_orders WHERE owner = ? AND corrected = 1",
        }},
        Window: "session",
    },
},
```

The YAML receipt and the Go declaration are compared at install: declare it
once, in the same shape, or the install stops.

## Field rules (each one is a gate, not a style note)

| Field | Rule | Failure |
|---|---|---|
| `kind` | `activity` or `outcome`; absent means `activity` | error |
| `label` + `query` | activity needs both; `SELECT COUNT(*)`, exactly one `?` when owner-scoped | error |
| `id` | unique, `snake_case`, within the recipe | error |
| `job` | one sentence, the user's goal, not the feature | error when empty |
| `beneficiary` | exactly one of `user` / `operator` | error |
| `success` / `total` | both present, each a valid count | error |
| table ownership | an outcome's counts name a table in `declaration.tables` | error |
| `counterbalance` | each a valid count whose filtered column appears in the recipe's code | error |
| `window` | `session`, `day` or `none` | error |
| `target` | absent unless the operator agreed a number | error when invented |

Why ownership is an error for outcomes: a ratio over a table you do not own
cannot be attributed to you, and it silently starts measuring something else the
day the owner renames a column. An activity count makes no claim a reader acts
on, so a stale one degrades to a missing number instead of a wrong claim.

Why `counterbalance` is a count and not a token: a token was a note to the
author that nothing could query. A count is the same `SELECT COUNT(*)` primitive
as every other reading, so the tension the declaration claims is **visible on
the page** — `8/10 · 80%` with `↳ Orders corrected 2` beneath it is a fact, and a
rise in corrections cannot be averaged away by a rise in volume.

## Where the numbers come from

Every reading is `COUNT(*)` over a table the recipe owns. That is the whole
vocabulary, and it is deliberate:

- **Not logs.** A reading over log text would need a second query engine
  (DuckDB-style JSON-over-files) inside a single-binary product, for a number
  the same write path can land in a row. If a fact matters enough to measure, it
  belongs in a column — that is why `security_events` exists.
- **Not an event stream.** A denominator the feature already stores does not
  need a collection pipeline, a retention policy or an opt-in toggle.
- **Not history.** If the ratio needs "as of last week", add the column that
  records it, not a pipeline that replays it.

## Getting the denominator right

- **Both halves are counts over the same population.** `total` is the
  population; `success` is the subset that went well. If `success` can exceed
  `total`, the queries are measuring two different things.
- **Zero denominator is "no data", not 0%.** The dashboard renders `—`; do not
  pre-divide, and never manufacture a `1` denominator to avoid the empty case.
- **Owner scoping mirrors the page.** When the feature's list is
  `WHERE owner = ?`, so is the ratio; a global count on a per-user panel is a
  leak of someone else's activity.

## What the model proposes

Ask the model for metrics and it proposes the whole entry: `kind`, the job
sentence, the beneficiary, both counts, and the counterbalance reading it thinks
qualifies the success number. Review it; do not accept it blind. Two checks a
model gets wrong most often:

1. **The beneficiary.** "Faster support replies" is the operator's job, not the
   user's. Optimizing the operator's number can make the user's worse.
2. **The counterbalance.** It must be the state that says the success number is
   not the whole story (a correction, an abandonment, a retry) — not a second
   success count wearing a different label.

## Asking the author

Do not invent a metric. Propose and wait:

1. State the job in the user's sentence.
2. Name the beneficiary.
3. Give the pair, with both labels.
4. Propose the counterbalance reading, and say whether the code already records
   that column or needs one.
5. Say what is a recommendation and what is already instrumented. Declare only
   after the operator confirms or edits.

Installing a recipe is the right moment: ask then, not in a follow-up.

## What the gates look like when it is right

- `recipe check <id>` prints one line per outcome (`id — success label / total
  label — job`) and lists the counterbalance columns it could not find in code.
  It has no database, so it reports the declaration, not a number.
- `recipe describe <id>` (and the MCP `recipe_describe` tool) shows the declared
  metrics, so an agent can find the existing instrumentation without reading the
  receipt.
- The dashboard card renders the ratio next to the activity counts, computed by
  the recipe's own SQL, with the counterbalance numbers beside it.

## Verifying a declaration

Two tests belong with the declaration, and both must go red before you trust
them:

```go
// The declaration passes every gate.
recipe.MetricProblems(d), recipe.OutcomeTableProblems(d)

// The ratio owes its numbers to a table the recipe owns: drop Tables and the
// outcome must stop validating.
d.Tables = nil
if len(recipe.OutcomeTableProblems(d)) == 0 { t.Fatal(...) }
```

`features/dashboard` has the mirror test for rendering: delete the outcome from
the fixture and the card must stop showing a ratio.

## Non-goals

- No event collection, no `events` table, no "enable analytics" flag.
- No percentage without its pair, and no score built from several outcomes.
- No query language beyond `COUNT(*)`: a reading that needs `SUM`/`AVG`/
  `GROUP BY` is describing a metric, or the value belongs in a column.
