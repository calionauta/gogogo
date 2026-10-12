# DagNats durable workflows

Pinned: DagNats v0.0.24 (go.mod). Bump procedure: `bin/check-dagnats-pin.sh`
fails when these drift — then diff upstream `CHANGELOG.md` between the two
tags, update the capability table below, and move the pin in the same commit.

DagNats (`internal/dagnats/`, `SCOPE:plugin`) is the durable multi-step
engine: declarative JSON workflows, state on the JetStream the binary
already runs, console on `:8090` (`DAGNATS_HTTP_ADDR`), opt-out via
`DAGNATS_ENABLED=false`. The reference workflow is onboarding
(`internal/dagnats/workflow.go` + `features/onboarding/`): greet →
await-first-todo (signal) → 3× create-todo → finalize.

Remove via the `SCOPE` header in `internal/dagnats/dagnats.go` (package +
`cmd/web/dagnats.go` + wiring calls).

## Capability table (DagNats v0.0.24)

Verified against the pinned module, not marketing prose. "Bounded
dynamism" is the design position (upstream ADRs `adr-001`, `adr-002`,
`adr-021`): every dynamic surface carries a numeric bound + validation.

| Capability | Mechanism | Evidence |
|---|---|---|
| Linear multi-step, durable | JSON DAG, `depends_on`, per-step output persisted | `OnboardingWorkflowJSON` survives handler refactors (names, not Go symbols) |
| Branch on result | `skip_if: ParentCond` — `==,!=,<,>,<=,>=` on parent step JSON output; pure, validated | `dag/condition.go`, `types.go`, `stepref.go:49` |
| Bounded loop | `StepTypeAgentLoop` + `Continue()/Checkpoint()` per iteration; `MaxIterations`/`MaxDuration` enforced | `internal/engine/step_handlers.go` (`checkLoopBounds`), ADR-002 |
| Retry/backoff | fixed / linear / exponential per step | `dag/retry.go` |
| Runtime-planned sub-DAG | planner steps emitting validated fragments (`MaxSteps` 1–100, `MaxDepth` 0–10, `AllowedTasks`) | `dag/planner.go`, `dag/dynamic.go` (`ValidateFragment`) |
| Agent-spawned runs | bounded `ControlPlane` capability + audit KV | ADR-021 (accepted, Phase A shipped) |
| Human/event gates | approval steps, `waitforevent` with match/dotpaths, `WaitForSignal` resume | `approval.go`, `waitforevent.go`; onboarding step 2 |
| Subworkflows | child runs from KV defs | `dag/subworkflow.go` |
| No free-form expressions | comparisons + dotpaths only; no CEL/eval | closed `validOps` set in `dag/condition.go` |
| No unbounded loops | every loop surface has a static ceiling | `checkLoopBounds`, planner bounds |

## Gotchas learned here (not upstream)

- Per-step `config` is NOT delivered to workers (engine dispatch omits
  it); per-step `metadata` IS (restored v0.0.22). Data rides the
  input/output chain instead — see `ExampleTodoTexts` in
  `internal/dagnats/workflow.go`.
- The console cannot create the first trigger on an empty bucket —
  `internal/dagnats/trigger_bootstrap.go` seeds a DISABLED def.
- `QueueSnapshotInterval` must be non-zero (upstream panics) — defaulted
  in `NewServer`.

## For LLM agents: how to add a workflow

1. Write the JSON def in `internal/dagnats/` (names as task contracts,
   data in the I/O chain, branches as `skip_if`, loops as agent-loop
   steps with explicit bounds).
2. Register task handlers on the queue worker (same names).
3. Validate through the engine (`make ci-local` runs the onboarding
   e2e; a red-proofed test per new workflow).
4. Upstream references (read before inventing): the module's `AGENTS.md`
   (TigerStyle rules), `docs/architecture/adr-002-durable-agent-loop.md`,
   `docs/architecture/adr-021-ad-hoc-agent-runtimes.md`,
   `docs/workflow-schema.json`. Operator surface: console (`:8090`),
   `cli/`, REST + NATS-micro control plane.

## Related

- [Stack in layers](stack-layers.md) — why DagNats and not Temporal.
- [Async layers](async-layers.md) — where workflows sit among the seven.
