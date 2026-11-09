# Recipe metrics and panel spec: the declarative dashboard and chat surfaces

Reviewed: 2026-11-09

This note records the evaluation behind the 2026-11-09 change that turned
dashboard metrics and chat panels from per-recipe code into declarations
(`Declaration.Metrics` + `Declaration.Panel`) evaluated generically — plus
the `recipe check` evaluator, the `doctor` wiring and the scaffold skeleton.
It is the follow-through of [Extensibility](extensibility-2026-11.md), which
already settled the copy-vs-declare split: code is copied and owned, schema
is declared and engine-applied. This note answers the narrower question that
change left open: **how far should the declaration go for the two derived
surfaces (dashboard counters and chat answer slices)?**

## The problem in one paragraph

Before this change the dashboard knew every recipe by name
(`LabelsFor` + a `switch` on recipe id in `features/dashboard/handlers/chi.go`)
and every chat panel was hand-written whole (a full `PanelProvider`
implementation per feature). A user-installed recipe therefore needed
**dashboard edits** to appear, and a new panel needed a full contract
written from scratch — a shallow copy-paste trap where the panel could drift
from the feature's own page markup (the todo panel lost the badge, author,
age, delete gate and native dialog while `/todo` kept them). The question
was how to make both surfaces declarative without inventing a second
registry.

## What was evaluated

### Declarative observability: one COUNT per metric vs. aggregated SQL builders

The generic path chosen executes **one COUNT query per declared metric**,
bound to the viewing owner (`?` when `OwnerBinds` defaults true, none when
false) — `Metric{Label, Query, Signal, OwnerBinds}` in
`internal/recipe/receipt.go`, executed by `declaredMetrics` in the dashboard
handler. The alternative — an aggregation framework where a recipe describes
a shape and the engine composes SQL — was rejected on KISS: with per-recipe
labels as the user-facing word and one owner per request, one query per
metric is simpler to review, to red-proof and to delete. A metric that is
not a `SELECT COUNT(*)` fails fast at boot via `MetricProblems`, not as a
broken SQL string at render time.

### Panel declaration: `PanelSpec` name/prompt/writable vs. full provider in YAML

`PanelSpec{Component, Prompt, Writable, Values}` carries only the
model-facing vocabulary. `Fill`/`Render` stay in code by necessity: `Render`
must call the feature's own page component (the "never a copy" rule,
[Native panels](../native-panels.md)). A YAML-only panel would force a
second markup to exist — the exact drift bug this change deleted. The
shipped shape is the pragmatic middle: **name/prompt/writable declaratively,
rows and markup in code**, validated fail-fast by `PanelProblems` (snake_case
component, non-empty prompt, exactly one writable prop — the slice).

### Shared base: `SlicePanel` vs. copy-paste contract

`features/genui/components/panel.go:SlicePanel` is the shared base: a recipe
writes the two closures (`List`, `Render`) and inherits `Component/Prompt/
Writable/Fill/Render`. Without it every panel re-implemented the same four
methods; with it the scaffold can emit a working `genui_panel.go` skeleton.
This is convention-over-config: the common shape is the default, the recipe
overrides only the slice it owns.

### Evaluator: `recipe check` + `doctor` + `make check-recipes` vs. lint only

`internal/installer/recipe_check.go` is a read-only, deterministic evaluator
(`RecipeCheckReport{id, healthy, score, errors, warnings, suggestions,
complexity, declaration, metrics, panel}`; score `100-10*errors-3*warnings`).
It reuses the existing drift/schema/policy/declaration/registry checks and
adds source-quality signals (complexity caps, wiring, tests, docs) that the
install path deliberately does not own. Wiring: `doctor` calls the same code
as one preflight item `recipe.declarations` (fail on structural errors, warn
on warnings); `make check-recipes` runs it in `ci-local-fast` and `ci-local`.
`generate` is deliberately not a home for it — codegen stays LLM-free.

### Registry: same repo vs. separate catalog repo

The catalog **is this repository**: `.recipes/*.yml` + `registry*.go` + the
generated `recipes.gen.go`. A separate registry repo was rejected because the
drift check compares the pinned receipt against the code in the **same
commit**; a second source of truth would need a sync protocol to stay honest,
for zero benefit at this scale. New recipes arrive as PRs.

## Decision and revisit trigger

- **Decision:** declare dashboard metrics as one COUNT per entry and the
  chat panel as name/prompt/writable; fill/render in code over `SlicePanel`;
  validate both fail-fast at boot and in `recipe check`; wire the evaluator
  into `doctor` and `make check-recipes`; keep the catalog in-repo.
- **Revisit trigger:** a recipe needing a non-COUNT metric (percentiles,
  windowed sums) or a panel with more than one writable prop. The current
  `validCount` (must `SELECT COUNT(*)`, exactly one `?`) and the
  exactly-one-writable rule are deliberate walls — crossing them means an
  explicit decision, not a silent extension.
- **What was rejected:** aggregation SQL builders (KISS), YAML-only panels
  (drift), `LabelsFor` switch (per-recipe code), separate catalog repo
  (second source of truth), a hosted checker (no boundary — `recipe check`
  is local by design).

## Status

Shipped 2026-11-09 (`#390`-`#393`). `LabelsFor` deleted; chat/credits keep
hook counts (MemStore/ledger have no COUNT query) and leave the generic path
the moment they gain a table.
