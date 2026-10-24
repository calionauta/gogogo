# DagNats durable workflows

Pinned: DagNats v0.0.30 (go.mod). Bump procedure: `bin/check-dagnats-pin.sh`
fails when these drift — then diff upstream `CHANGELOG.md` between the two
tags, update the capability table below, and move the pin in the same commit.

DagNats (`internal/dagnats/`, `SCOPE:plugin`) is the durable multi-step
engine: declarative JSON workflows, state on the JetStream the binary
already runs, console on `:8090` (`DAGNATS_HTTP_ADDR`), opt-out via
`DAGNATS_ENABLED=false`. The browser-facing console is `/console/`
(alias `/dagnats/`), and it is operator-only: admin **and** an
`ADMIN_EMAILS` match, always on regardless of `RBAC_ENABLED`. An empty
list closes it. The reference workflow is onboarding
(`internal/dagnats/workflow.go` + `features/onboarding/`): greet →
await-first-todo (signal) → 3× create-todo → finalize.

Remove via the `SCOPE` header in `internal/dagnats/dagnats.go` (package +
`cmd/web/dagnats.go` + wiring calls).

## Capability table (DagNats v0.0.30)

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
| Multi-node cluster + leases | replicated JetStream, route/leaf config, `api.leases.*` request/reply, leader-routed loads | `server/config.go`, `internal/lease/`, `dagnatstest/cluster_test.go` |
| Observability | OpenTelemetry traces/metrics/logs, OTLP export | `internal/observe/`, `server/config.go` (`OTLPEndpoint`) |
| No free-form expressions | comparisons + dotpaths only; no CEL/eval | closed `validOps` set in `dag/condition.go` |
| No unbounded loops | every loop surface has a static ceiling | `checkLoopBounds`, planner bounds |

## Behavior changes between the template's old pin and v0.0.30

Verified against the module's `CHANGELOG.md`, not prose. Each one changes
engine behavior under an UNCHANGED workflow def, which is why it is written
down here rather than left to the reader of the release notes:

- **`OutputSchema` is now enforced (0.0.30).** When a run completes, each
  terminal step's output is validated against it; a mismatch fails the run
  with the violation as the failing step's error plus a `workflow.failed`
  event, and an unparseable schema is rejected at register time. A def that
  carried an aspirational schema and produced loose output used to pass; it
  now fails. Defs without a schema behave as before.
- **A step is no longer stranded when the engine dies mid-dispatch
  (0.0.30).** The dispatch save marks the step as not-yet-sent and a second
  save clears it once the publish lands, so a redelivered event (or the
  reconciler's pass) sends every step still marked. Cost: one extra snapshot
  write per dispatch.
- **Cron `backfill: true` now replays (0.0.28).** A missed fire replays once
  per matching minute since the last run, oldest first, capped at 100 per
  trigger. It used to be stored and ignored, so a restarted engine silently
  skipped the window.
- **Subject-trigger `debounce` now takes effect (0.0.28).** A burst inside
  `period` starts ONE run with the last payload; `timeout` caps how long a
  window extends. It used to be validated and ignored, so every message
  started a run — a def relying on that accidental behavior now sees fewer,
  later runs.
- **`SetupAll` provisions `triggers`, `trigger_state`, `signals`,
  `checkpoints`, `concurrency_runs` (0.0.28)** and `dagnats-engine` /
  `dagnats-api` create them too, instead of depending on `dagnats serve`
  having run first.
- **KV buckets created outside `SetupAll` are no longer single-replica in a
  cluster (0.0.28)**, and provisioning never LOWERS an existing stream's or
  bucket's replica count (0.0.29) — a lower factor keeps the existing count
  and logs a warning; reduce replicas with the `nats` CLI if you mean it.
- **Replica factor comes from the cluster, not a guess (0.0.29).**
  `dagnats-engine` and `dagnats-api` read
  `DAGNATS_NATS_JETSTREAM_REPLICAS` like `dagnats serve` and derive it from
  the cluster when unset; a booting cluster that refuses three replicas
  fails startup naming that variable instead of silently falling back to one.
- **A run whose pinned def was deleted can be cancelled (0.0.29).** It used
  to fail with "pinned to workflow def version ... which no longer exists",
  go to the dead-letter stream and stay `running`; it is now cancelled like
  any other, releasing its tasks, waiters, locks and child runs.
- **Malformed defs fail validation instead of panicking** at run start, and
  an incomplete `cancel_on` entry is rejected upfront (0.0.28).

## Gotchas learned here (not upstream)

- Per-step `config` is NOT delivered to workers (engine dispatch omits
  it); per-step `metadata` IS (restored v0.0.22). Data rides the
  input/output chain instead — see `ExampleTodoTexts` in
  `internal/dagnats/workflow.go`.
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
