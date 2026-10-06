# The Todo example

The Todo app is the reference implementation. Every pattern the template
teaches is visible here first — copy this shape when you add your own feature.

## What it ships

- Full CRUD via PocketBase.
- Reactive frontend with Datastar + the active skin (DaisyUI by default;
  BasecoatUI switchable — see [UI skins](ui-skins.md)).
- **Database actions stream through PocketBase realtime.** Todo
  `create`/`toggle`/`delete` fire PocketBase record events; each subscribed
  client re-fetches the fragment and morphs `#todo-list`. Delivery is per-user
  scoped by the collection's `owner` rule
  (`@request.auth.id != '' && owner = @request.auth.id`), so a client only
  receives events for its own records. The SSE Hub is reserved for ephemeral
  signals (success/retry toasts, live clients count, AI suggest) and the
  originating client's own synchronous patch.
- Stacked toast notifications (auto-dismiss, manual close, progress bar).
- **UI sounds** via cuelume — see [UI sounds](ui-sounds.md).
- Async jobs: the queue handles `toast`, `suggest_result` and `retry_demo`
  jobs (`internal/queue`), and `features/todo`'s own tests use a
  `todo_created` job to exercise the worker → SSE path. **Todo CRUD itself does
  not enqueue**: `handleCreate` re-renders synchronously (`emitToast(sse,
  "Added", "success")`), because the broadcaster already pushes record changes
  to other clients and a queue hop would only add latency to a fast local
  mutation.
- Retries with exponential backoff and jitter (`internal/queue/retry.go`,
  retry-go v4) — SSE-aware: a retry emits a `lastRetry` signal so the UI can
  show "retrying…".
- **`WelcomeOnboarding` DagNats workflow** (always compiled) that creates 3
  example todos via durable steps — kill the server mid-run, restart, and it
  resumes at the last incomplete step. The workflow is declarative JSON
  (`internal/dagnats/workflow.go`), so renaming Go handlers never orphans an
  in-flight run.
- **Admin unlock** via age + `~/.secrets/`. When `ADMIN_UNLOCK_TOKEN` is set,
  the UI shows a "Clear all" form; the handler compares constant-time and
  clears all todos on match. Demonstrates the age flow end-to-end.
- **AI suggest** via GoAI, or keyless via `SIMULATE_LLM`.
- Tests run with `-race`.

## The contract to imitate

1. **Pure HTTP + Datastar** for the user-facing surface.
2. **goqite job** for any work that takes more than ~50ms (LLM, email, exports).
3. **SSE toast** for async feedback to the originating client via `clientID`
   routing.
4. **age-encrypted secret** if the feature needs a credential.

Every existing feature — toast on create, AI suggest, admin unlock, DagNats
onboarding — follows this exact shape.

## AI suggest

When `GOAI_API_KEY` is set, the input gets a **Suggest** button that enqueues
an async suggest job and streams the 3 completions back via SSE. It talks to
whatever OpenAI-compatible provider `GOAI_BASE_URL` / `GOAI_MODEL` point at.
Retries with exponential backoff use the same `internal/queue/retry.go` as the
SSE toast path.

The stepper UI (`aiStep` / `aiPending` / `aiPhase` signals) is **kept
independent** from the Queue + Retry demo's stepper signals (`techStep` /
`techPhase`), so running one never lights the other.

For a **keyless** demo of the exact same queue + retry path, `SIMULATE_LLM` is
on by default in dev (opt out with `SIMULATE_LLM=false`): a "Suggest
(simulated)" button enqueues a job that hits an in-process fake LLM scripting
500 → 200 + delay, so you can watch the retry feedback toasts (enqueued →
attempt failed → slow → result).

If neither `GOAI_API_KEY` is set nor `SIMULATE_LLM` is enabled, the AI suggest
route is **not registered** and the UI button is hidden. The Todo example keeps
working — AI is opt-in, not required.

## The onboarding workflow

`WelcomeOnboarding` is the durable-workflow demo: on first login it creates
three example todos through DagNats. Because the workflow is declarative JSON
over JetStream, you can stop the process mid-run, restart, and it picks up at
the last incomplete step.

Titles ride the DAG's input/output chain rather than step config, because the
DagNats engine's live dispatch path does not populate per-step `config` in the
task payload. (Per-step `metadata` *is* delivered as of DagNats v0.0.22, but
the input/output chain is what guarantees each step sees only the todos that
remain — migrating titles to `metadata` would hand every step all three.)

## Related

- [Features](features.md)
- [Six async layers](async-layers.md) — why PB realtime and not the SSE Hub for CRUD.
- [Adding your own feature](getting-started.md#adding-your-own-feature)