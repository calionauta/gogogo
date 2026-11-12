# Outcomes and metrics — declaring what the job measured

Read this when a recipe has to say **whether it worked**, not just how much
happened. It is the full contract behind the `metrics` / `outcomes` fields; the
engine enforces it at boot, at install and at `recipe status`, so a declaration
that disagrees with the code never ships.

## Metric vs outcome

- A **metric** counts activity. `Orders confirmed: SELECT COUNT(*) FROM
  ext_coffee_orders WHERE owner = ?`. One number, no denominator, no claim.
- An **outcome** answers a job for a beneficiary, as a **pair of counts**:
  success over total. It renders as `2/2 · 100%`.

An outcome is not an event stream, not a funnel and not a score. If you find
yourself needing history to compute it, the recipe is missing a **column**, not
a pipeline.

## Schema

```yaml
metrics:
  - label: Orders confirmed
    query: "SELECT COUNT(*) FROM ext_coffee_orders WHERE owner = ?"

outcomes:
  - id: order_without_repair
    job: Receive the coffee you asked for without fixing a misread order
    beneficiary: user            # user | operator — never both
    success:
      label: Orders without repair
      query: "SELECT COUNT(*) FROM ext_coffee_orders WHERE owner = ? AND corrected = 0"
    total:
      label: Orders confirmed
      query: "SELECT COUNT(*) FROM ext_coffee_orders WHERE owner = ?"
    counterSignals: [corrected]  # tokens your code must actually produce
    window: session              # session | day | none
    target: 0.9                  # optional; null unless the operator agreed a number
```

The same declaration in Go, in the recipe's `Declaration()`:

```go
Metrics: []recipe.Metric{{
    Label: "Orders confirmed",
    Query: "SELECT COUNT(*) FROM ext_coffee_orders WHERE owner = ?",
}},
Outcomes: []recipe.Outcome{{
    ID:          "order_without_repair",
    Job:         "Receive the coffee you asked for without fixing a misread order",
    Beneficiary: "user",
    Success: recipe.Metric{
        Label: "Orders without repair",
        Query: "SELECT COUNT(*) FROM ext_coffee_orders WHERE owner = ? AND corrected = 0",
    },
    Total: recipe.Metric{
        Label: "Orders confirmed",
        Query: "SELECT COUNT(*) FROM ext_coffee_orders WHERE owner = ?",
    },
    CounterSignals: []string{"corrected"},
    Window:         "session",
}},
```

The YAML receipt and the Go declaration are compared at install: declare it
once, in the same shape, or the install stops.

## Field rules (each one is a gate, not a style note)

| Field | Rule | Failure |
|---|---|---|
| `id` | unique, `snake_case`, within the recipe | error |
| `job` | one sentence, the user's goal, not the feature | error when empty |
| `beneficiary` | exactly one of `user` / `operator` | error |
| `success` / `total` | each a valid metric: `SELECT COUNT(*)`, exactly one `?` when owner-scoped | error |
| table ownership | every query names a table in `declaration.tables` | error |
| `counterSignals` | each token appears in the recipe's own code | error |
| `window` | `session`, `day` or `none` | error |
| `target` | absent unless the operator agreed a number | error when invented |

Why ownership is an error: a ratio over a table you do not own cannot be
attributed to you, and it silently starts measuring something else the day the
owner renames a column.

Why `counterSignals` is an error: the token is the state the ratio is limited
by. If no write path records it, the success count can never move for the reason
the outcome claims. The token check is textual — `recipe check` knows the word
appears, not that the path is right — so the code must also ship a test that
calls the write path and pins the recorded value.

## Getting the denominator right

- **Both halves are counts over the same population.** `total` is the
  population; `success` is the subset that went well. If `success` can exceed
  `total`, the queries are measuring two different things.
- **Zero denominator is "no data", not 0%.** The dashboard renders `—`; do not
  pre-divide, and never manufacture a `1` denominator to avoid the empty case.
- **Owner scoping mirrors the page.** When the feature's list is
  `WHERE owner = ?`, so is the ratio; a global count on a per-user panel is a
  leak of someone else's activity.

## Counter signals: the state your code produces

A counter signal is a **fact the write path records**, so the outcome can show
how often it went wrong:

```yaml
# in the confirm path
corrected := stored != previewed   // the row was stored as something else
```

Store it as a column on the row the recipe already writes
(`ext_coffee_orders.corrected`), then let the success query filter on it. That
is the whole mechanism: no event table, no collection toggle, no background
aggregation, and the number cannot drift from the rows.

Pick **one** signal, the one the job's failure mode would show up in. Two
signals usually means the outcome is two outcomes.

## Asking the author

Do not invent an outcome. Propose and wait:

1. State the job in the user's sentence ("receive the coffee you asked for
   without fixing a misread order").
2. Name the beneficiary.
3. Give the pair, with both labels.
4. Propose the one counter signal, and say whether the code already records it
   or needs a column.
5. Say what is a recommendation and what is already instrumented. Declare only
   after the operator confirms or edits.

Installing a recipe is the right moment: ask then, not in a follow-up.

## What the gates look like when it is right

- `recipe check <id>` prints one line per outcome (`id — success label / total
  label — job`) and lists the counter signals it could not find in code. It has
  no database, so it reports the declaration, not a number.
- `recipe describe <id>` (and the MCP `recipe_describe` tool) shows the declared
  metrics and outcomes, so an agent can find the existing instrumentation
  without reading the receipt.
- The dashboard card renders the ratio next to the metric, computed by the
  recipe's own SQL.

## Verifying a declaration

Two tests belong with the declaration, and both must go red before you trust
them:

```go
// The declaration passes every gate.
recipe.MetricProblems(d), recipe.OutcomeProblems(d)

// The ratio owes its numbers to a table the recipe owns: drop Tables and the
// outcome must stop validating.
d.Tables = nil
if len(recipe.OutcomeProblems(d)) == 0 { t.Fatal(...) }
```

`features/dashboard` has the mirror test for rendering: delete the outcome from
the fixture and the card must stop showing a ratio.

## Non-goals

- No event collection, no `events` table, no "enable analytics" flag.
- No percentage without its pair, and no score built from several outcomes.
- No query language beyond `COUNT(*)`: an outcome that needs
  `SUM`/`AVG`/`GROUP BY` is describing a metric, or the value belongs in a
  column.
