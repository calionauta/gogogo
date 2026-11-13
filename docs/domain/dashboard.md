# dashboard

**Owns.** The aggregate view: per-owner counts and ratios read from rows other slices own, including the declared outcomes a recipe publishes.
**Does not own.** The rows it counts, the queries that define an outcome (the recipe declares them), or any write — the dashboard is read-only.

## Language

| term | means | not |
|---|---|---|
| outcome | a declared pair of queries: successes over a total, per owner | a made-up KPI |
| ratio | success total divided by the outcome total | a count of events |
| card | one rendered number with its label and window | a chart library |

## Invariants

- Every query is owner-scoped: the page shows the caller's rows, never the instance's.
- An outcome is read from the declaration; the page never invents a numerator or denominator.
- A unit that is not installed has no card, and no placeholder number.
- The page performs no writes and no periodic fetch.
