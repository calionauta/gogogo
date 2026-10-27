// SCOPE:plugin — DagNats durable workflow engine
package dagnats

// ReminderWorkflowJSON is the durable reminder workflow. The delay is an engine
// `sleep` step resolved from the run input (`due_at`, RFC3339), so it survives a
// restart and holds no goroutine while it waits — the reason this feature uses
// the engine instead of a timer.
//
// The `seed` step exists because of how the engine resolves input: a step with
// one dependency receives that dependency's OUTPUT, and a sleep step produces
// none. Seeding the run input into a normal step first (the onboarding-greet
// pattern) and giving `deliver` two dependencies is what carries the payload
// across the wait: ResolveInput then hands the worker
// {"seed": {…run input…}, "wait": null}.
//
// `idempotency_key` names the input field the engine dedups run starts on, and
// the delivery row keeps a second guard at the database (`notifications_txn`),
// because the engine only dedups starts, not a retried step.
const ReminderWorkflowJSON = `{
  "name": "reminder",
  "version": "1.0",
  "idempotency_key": "transaction_id",
  "steps": [
    {
      "id": "seed",
      "task": "reminder-seed",
      "depends_on": []
    },
    {
      "id": "wait",
      "type": "sleep",
      "config": {"until_input_path": "due_at"},
      "depends_on": ["seed"]
    },
    {
      "id": "deliver",
      "task": "notification-deliver",
      "depends_on": ["seed", "wait"]
    }
  ]
}`
