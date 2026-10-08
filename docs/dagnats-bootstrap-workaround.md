# DagNats trigger console: bootstrap workaround

> **This page documents a workaround for an upstream bug, not a feature.**
> Read the *Re-evaluate this* section before assuming it is still needed.

## The symptom

On an installation that has never created a trigger, the DagNats console at
`/dagnats/console/triggers` refuses to create the first one:

```
create failed: 500 lookup failed: nats: no keys found
```

The console renders fine, the workflow list works, and the error only appears
when you submit the trigger form. After the first trigger exists, everything
behaves normally — which is what makes it a confusing first-run failure.

## The cause

A defect in DagNats v0.0.24. Its trigger service reads the trigger KV bucket
before writing, to check for a duplicate id, via `listTriggersInner` in
`internal/api/service_triggers.go`:

```go
keys, err := s.triggerKV.Keys(ctx)
if err != nil {
    return nil, err           // <- ErrNoKeysFound returned raw
}
```

When the `triggers` bucket exists but is empty, `Keys` returns
`jetstream.ErrNoKeysFound`, and that error is propagated. The console turns it
into a 500 and never reaches the write.

This is an **inconsistency inside upstream**, not a design decision: 25 other
call sites in the same repository handle exactly this condition as benign,
including `checkHTTPRouteConflict` — which lives inside `CreateTrigger` itself
and is reached *after* the failing list call.

The result is a deadlock for a new installation: the console is the only
surface that can create a trigger, and it requires the bucket to be non-empty
first. Nothing in the UI can ever break the cycle.

Reproduced against a real engine (embedded NATS + HTTP console): two
consecutive `POST /console/triggers` both return 500 and the bucket stays
empty.

## The workaround

On boot, the app seeds the bucket with **one real, disabled trigger
definition** if — and only if — the bucket is empty:

```json
{
  "id": "_bootstrap",
  "workflow_id": "onboarding",
  "enabled": false,
  "cron": { "expression": "0 3 * * *" },
  "source": "bootstrap"
}
```

Everything about that choice is deliberate:

| Property | Why |
|---|---|
| `enabled: false` | It can never fire. No workflow is triggered by the seed. |
| A real `TriggerDef`, not a marker key | The console renders **every** KV entry as a trigger row. A `_marker` key shows up in the UI as a phantom trigger of kind `unknown`. A valid disabled definition renders as an ordinary trigger. |
| `workflow_id` points at a real workflow | The row is valid rather than a dangling reference. |
| `source: "bootstrap"` | Distinguishable from console- and CLI-created triggers, using upstream's own `Source` field. |
| Written only when the bucket is empty | An existing installation is never touched, and repeated boots are no-ops. |

The seed is visible in the trigger list. **Once you have created your own
trigger, you can delete `_bootstrap` from the console** — it is ordinary data.
Deleting it does not re-break anything, because the bucket is non-empty by
then.

The implementation lives in `internal/dagnats/trigger_bootstrap.go`, called
from `cmd/web/main.go` after the NATS connection is established.

## Disabling and removing

**Disable** without code changes:

```bash
DAGNATS_TRIGGER_BOOTSTRAP=false
```

Then delete the `_bootstrap` row from the console if it is already there.

**Remove entirely** once upstream is fixed. Three edits:

1. Delete `internal/dagnats/trigger_bootstrap.go` and its test
   (`trigger_bootstrap_test.go`).
2. Drop the `TriggerBootstrap` field from `config/config.go` (the
   `DagNats` struct, its `envBool` line, and the env-var comment).
3. Remove the `ensureTriggerBootstrap()` call and function from
   `cmd/web/main.go` and `cmd/web/dagnats.go`.

Nothing else references it. The seeded row is ordinary KV data, so it can stay
or be deleted independently.

## Re-evaluate this

**Check this on every DagNats upgrade.** The workaround costs a boot-time KV
read and one placeholder row; it should not outlive the bug.

To check whether upstream has fixed it, run this two-step test against a clean
engine:

```bash
# 1. Start the app with the workaround OFF and a fresh data dir.
DAGNATS_TRIGGER_BOOTSTRAP=false DAGNATS_STORE_DIR=/tmp/dagnats-check make dev

# 2. Try to create a trigger in the console:
#    http://localhost:8090/console/triggers
```

If the create **succeeds**, upstream is fixed — remove the workaround as
described above. If it still reports `no keys found`, the workaround is still
required.

The failing code is `listTriggersInner` in
`github.com/danmestas/dagnats/internal/api/service_triggers.go`. The fix
upstream is three lines: return an empty slice when
`errors.Is(err, jetstream.ErrNoKeysFound)`, matching what the rest of the
package already does.

**Upstream report:** [danmestas/dagnats#745](https://github.com/danmestas/dagnats/pull/745)
— a PR with that fix plus a regression test. It also documents the blast
radius: four console actions guard on the same call (`create`, `update`,
`delete`, `toggle`), and the reason the maintainer never hit it (the CLI and
`workflow register` write through `CreateTrigger` or straight to the KV, so
neither touches the raising call).

### When #745 is merged and released

1. Bump `github.com/danmestas/dagnats` in `go.mod` to the release containing it.
2. Run the two-step test above with the workaround **disabled**
   (`DAGNATS_TRIGGER_BOOTSTRAP=false`). If the create succeeds, the bug is gone.
3. Remove the workaround in the same commit as the bump — see the three edits
   above. Do not leave it for a follow-up; a workaround that outlives its bug
   reads as a requirement to the next person.
4. Drop the row from the workaround table in `AGENTS.md`.
5. Record the re-check in the bump commit body, whether or not it was needed.

## Related

- [Async layers](async-layers.md) — where DagNats sits among the seven layers.
- [Troubleshooting](troubleshooting.md) — other first-run failures.
- [Configuration](configuration.md) — `DAGNATS_TRIGGER_BOOTSTRAP` and the rest
  of the DagNats settings.
