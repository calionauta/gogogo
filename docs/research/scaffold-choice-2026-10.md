# Scaffold choice: everything the installer can ask, and the three lists that answer it

Reviewed: 2026-10-30

**Question.** The scaffold is where a project chooses what enters the tree, but
`perf` (statsviz + pprof) is a plugin the installer never offers. Is that one
unit, or does the same gap exist across the registry? What would it take to make
every capability an explicit keep-or-drop choice?

**Short answer.** Three lists answer "what does the scaffold offer", and only two
of them agree. The fix is one `Scaffold` class in the capability registry, a trim
unit for every `default`/`opt-in` capability, and a prompt/MCP/docs generated from
that class instead of from `manifestUnits` and `Offered` separately.

## The map

Entry points and the data they read, from `ripwire`, `cymbal` and `xerj`:

| Layer | Where | What it decides |
|---|---|---|
| CLI | `cmd/gogogo` → `installer.Run` (`internal/installer/run.go`) | flags, interactive form, plan, apply, prove |
| MCP | `cmd/gogogo-mcp` → `toolCatalog` (`trim_plan`, `trim_apply`, `capabilities_list`, `advise_stack`, `add_unit`) | the same engine, JSON schemas |
| Interactive form | `internal/installer/prompt.go` `promptForm` → `askKeep` | project, owner, **plugins**, **features** |
| Unit list | `internal/installer/manifest.go` `manifestUnits` = `buildManifestUnits()` (`manifest_units.go`) | the units the prompt, plan and trim actually use |
| Unit mechanics | `manifest_units.go` `trimUnit` (`mainStrips`, `extraStrips`, `extraDrops`, `replaces`, `goModDrops`) | what a trim deletes and which lines it strips |
| Registry | `internal/capabilities/registry.go` `All`, `capabilities.go` `OfferedIDs` | kind, summary, `Offered`, `RuntimeOff`, `Reason` |
| Plan/preview | `internal/installer/plan.go` `scaffoldPlan`, `buildPlan`, `printPlanText` | kept/dropped units, warnings, strips |
| Inverse | `internal/installer/add.go` + `add_spans.go` | restore a trimmed unit (`gogogo add`) |
| Drift gate | `internal/installer/check.go` (`gogogo --check`) | markers still match the tree |
| Docs | `docs/unit-contracts.md` (generated), `docs/getting-started.md`, `docs/features.md`, `docs/scope-taxonomy.md`, `site/index.html` | what a reader believes the scaffold offers |

The prompt is narrower than the registry: `askKeep` iterates `manifestUnits` and
filters by `Kind`; it never reads `Capability.Offered`. The plan and apply use the
same `manifestUnits`. `OfferedIDs` exists to keep the registry and the installer
in sync (`TestOfferedSetMatchesInstaller`), so `Offered` is a mirror of the
installer, not the source of truth.

## The gap

Three sources disagree by design, not by accident:

1. **Registry** — one `Capability` per unit with an `Offered` boolean. Every card
   says `Trim: installer` or `Trim: manual (no installer unit)`
   (`docs/unit-contracts.md`; counts live in its
   [registry at a glance](../unit-contracts.md#registry-at-a-glance)).
2. **Installer** — `manifestUnits` is the only set `--plugins`/`--features`,
   `trim_plan` and `trim_apply` accept.
3. **Landing** — the trim plan previews every plugin, checkboxes included, and
   says "a preview, not a live switch" (`site/index.html`).

The consequences:

- **No opt-in class.** `perf` defaults off (`PERF_ENABLED=false` in production),
  but `Offered: false` means "not asked"; if it became `Offered: true` it would be
  kept by default, which is the opposite of its default. The model has
  keep-by-default or manual, nothing in between.
- **`Offered: false` is overloaded.** It means "runtime flag only" for `nats`,
  `llm`, `offline-sync`; "manual removal" for `perf`, `delivery`, `notifications`,
  `account`; and "core, never offered" for `appconfig`. A reader cannot tell which
  without the `Reason` prose.
- **The prompt asks by kind, not by choice.** There is no "default on / default
  off" label, no consequence summary beyond the unit name, and `--yes` keeps every
  trim unit.
- **The landing promises more than the prompt.** A checkbox for a manual unit is a
  choice the installer cannot apply.
- **`add` only inverts offered units.** A manual unit has no restore path, so
  "trim it by hand" is a one-way edit.
- **Docs disagree with the prompt.** `getting-started.md` says four questions and
  lists the units; `features.md` lists runtime opt-outs but not scaffold class;
  `unit-contracts.md` is the only place with `manual` vs `installer`.

## Refactor: one class, one list, one prompt

**1. `Scaffold` on `Capability`.** Add a closed enum and make it the single
machine-readable answer:

| Class | Prompt | Trim unit | Example |
|---|---|---|---|
| `core` | never | never | `auth`, `server`, `database` |
| `default` | asked, kept by default | required | `dagnats`, `credits`, `sounds` |
| `opt-in` | asked, dropped by default | required | `perf`; candidates: `delivery`, `offline-sync` |
| `manual` | not asked, documented reason | not required | `nats`, `llm`, `notifications`, `account` |

`OfferedIDs` becomes derived (`default` + `opt-in`) and `Offered` is removed or
kept as a computed accessor. One invariant test replaces
`TestOfferedSetMatchesInstaller`: every `default`/`opt-in` has a `trimUnit`, every
`trimUnit` has a capability, and no `core`/`manual` has one.

**2. `perf` first.** It is the smallest opt-in candidate: one dir (`internal/perf`),
three files (`internal/server/perf.go`, `internal/server/perf_test.go`,
`internal/ui/perf.go`), one `goModDrops` (`statsviz`), and strips in `chi.go`,
`operator.go`, `shell.templ`, `controls.templ`, `config.go`, `policy.csv`,
`coverage_test.go` and the registry entry. The trim proof already exists:
`trim_build_test` regenerates templ and vets the tree. `add perf` needs the inverse
spans; the `perfPageMiddleware` block and the menu block are the two hand-written
shapes.

**3. Prompt by class.** Keep the four questions, but split the last two into
"keep by default" and "opt-in" groups, showing the default for each. `none` drops
all offered units; `--yes` uses the defaults (not "keep every trim unit");
scripted adds `--opt-in perf` (or reuses `--plugins perf` with the class deciding
the default). The plan prints the class and the runtime switch, not just strips.

**4. MCP and docs read the same class.** `capabilities_list` returns `scaffold`;
`trim_plan` accepts the same keep/opt-in lists and returns `scaffold` per unit;
`add_unit` refuses a `manual` unit with the registry `Reason`. `unit-contracts.md`
gains a `Scaffold:` line; `features.md` gains the column; `getting-started.md`
explains default vs opt-in; the landing plan is generated from `scaffold` and stops
offering manual units as checkboxes.

**5. Then widen.** Evaluate `delivery` (the notify adapter is small; the engine is
external), `offline-sync`, `notifications`, `nats`, `llm` and `account` as opt-in
or manual, one trim unit at a time, each with a `trim_build_test` proof. The
`manual` class stays for units whose removal is a refactor, not a strip — the
taxonomy already says so (`scope-taxonomy.md`).

## What improves

- The scaffold becomes the one place a project chooses what enters, with defaults
  and consequences in one prompt.
- The landing stops promising choices the installer cannot apply.
- `Offered` stops being a boolean that means three things.
- `add` gains a symmetric restore path for every offered unit.
- Agents get the same class from `capabilities_list` and `trim_plan` that humans
  see in the prompt.

## Risks and gates

- **Prompt growth.** Grouping by default/opt-in keeps it two menus, not 40
  questions; `--yes` must mean "defaults", which changes current behavior.
- **Trim proof cost.** Each new unit adds a repo copy + templ + vet to
  `trim_build_test`; the suite is host-gated, but CI pays it.
- **`add` inverse.** A strip without a span anchor is not reversible; the
  `perfPageMiddleware` and menu blocks need add-side anchors before the unit ships.
- **Docs drift.** The generated card and the landing must read `Scaffold`; a prose
  count is still a `check-docs` failure.

## Status

Implemented for `perf` (2026-10-30): `ScaffoldOptIn`, a trim unit with the
dependency drop, and the prompt's fifth question (`--opt-in` in scripted mode).
The trim is one-way: `gogogo add perf` prints the exact wiring lines because the
manifest's `extraDrops`/`replaces` are not add-anchored yet. The remaining
`manual` units are the next phase, one trim unit at a time.

## Related

- [Scope taxonomy](../scope-taxonomy.md) — core/plugin/feature and the
  "plugin does not promise a one-line delete" clause.
- [Unit contracts](../unit-contracts.md) — the per-unit card and `Trim:` line.
- [Getting started](../getting-started.md) — the current four questions.
- [Dependency principles](../dependency-principles.md) — the trim proof and the
  one-line trade-off.
