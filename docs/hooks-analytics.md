# Hooks, events and analytics

One in-process event bus. Hooks and analytics are both **sinks** on it, so a
deployment that wants neither pays a non-blocking channel send and nothing
leaves the binary.

This is deliberately not a trigger engine. A recipe cannot inject a step into a
core handler, and markup cannot execute code: the surface is a closed set of
event names, a webhook, and one provider seam.

## The seam

- `internal/events` owns `Event` (`name`, `at`, `fields`), the `Bus` and the
  `Sink` interface.
- `Bus.Emit` never blocks the request. A full buffer drops the event and logs a
  warning; request latency wins over a lost analytics event.
- `Bus` is constructed and started by the composition root (`internal/server`),
  closed on shutdown, and handed to recipes as `register.Deps.Events`.
- Delivery is **best-effort**. `security_events` remains the durable audit
  record and the access log remains the per-request record; the bus adds
  outbound facts, it never replaces either.

## What emits today

| Event | When | Fields |
|---|---|---|
| `capability.interaction` | a mounted capability serves a **mutating** request | `capability`, `method`, `path`, `status`, `duration_ms` |
| `recipe.toggled` | an admin flips a recipe's runtime switch | `recipe`, `enabled`, `actor` |
| `ui.<name>` | the browser beacon posts an allowlisted `data-analytics-event` name | `path`, `actor` |

Reads are not events: they are the access log's job (`HTTP_ACCESS_LOG`, plus
the always-on line for capability-owned requests). Emitting every request would
turn a webhook into a firehose and an analytics project into a bill.

A recipe publishes its own facts with `deps.Events.Emit` **after** its write
commits, naming them with its own prefix (`kiosk.confirmed`). The receipt does
not declare `events:` yet — that needs a consumer and a gate before it becomes
contract.

Auth lifecycle (`login`, `logout`, role change) is **not** on the bus: it is
already in `security_events` and the access log, and putting it here means
`auth.Auth` holds the bus — a contract change worth making only when a consumer
needs it. Install/uninstall happen in the evolution CLI, a separate process
with no bus; the receipt and its audit record are the trace there.

## Sinks

| Switch | Sink | Effect |
|---|---|---|
| `EVENTS_WEBHOOK_URL` | `events.Webhook` | POSTs every event as JSON, HMAC-signed |
| `ANALYTICS_PROVIDER=posthog` (plus `ANALYTICS_KEY`) | `analytics.PostHog` | server-side `/capture/` POST per event |

Both are off by default. `ANALYTICS_PROVIDER` accepts `none` (default) or
`posthog`; an unknown value warns and falls back to `none`, and a provider with
no key falls back to `none`, so a typo never sends events somewhere unexpected.

### Webhook contract

`POST <EVENTS_WEBHOOK_URL>` with:

- body: `{"name": "...", "at": "...", "fields": {...}}`
- `X-Gogogo-Event: <name>`
- `X-Idempotency-Key: <name>-<RFC3339Nano>`
- `X-Gogogo-Signature: sha256=<hex HMAC-SHA256 of the raw body>` when
  `EVENTS_WEBHOOK_SECRET` is set

A 2xx acknowledges; anything else is retried twice with a short backoff, then
dropped with a warning. The URL is never logged: it can carry a token in its
path.

**Not built yet:** a durable outbox. Delivery happens on the bus goroutine, so a
receiver that hangs delays every event behind it (bounded by the client timeout
and the retry budget). The documented next step is an outbox row written in the
same transaction and drained by the existing queue, which is what makes delivery
survive a crash. Until then: best-effort, visible in the log.

## The UI affordance

Only the browser knows a click happened, so one script and one attribute cover
it — no page ships its own tracker, and no vendor script is embedded in a
template.

```html
<button data-analytics-event="todo.create">…</button>
```

- `web/resources/static/analytics.js` is loaded by the shell **only when a
  provider is configured** (the server seeds `ui.WithAnalytics` per navigation).
- A click on the closest `[data-analytics-event]` posts
  `{"name": "...", "path": "..."}` to `POST /api/analytics`.
- The endpoint requires a session, is bounded (4 KB body, 200-char path), and
  accepts only `^[a-z][a-z0-9_.-]{1,63}$`. The actor comes from the session,
  never from the payload.
- The event is named `ui.<name>` on the bus.
- The route is mounted only when analytics is enabled, so a tree with analytics
  off has no beacon surface at all. RBAC row: `p, user, /api/analytics, POST`.

There is no client-side pageview: a page render is already a server request, and
it is already logged.

## What this is not

- **No arbitrary hooks.** No `onAnyFunction` registry, no runnable markup, no
  tenant-scoped hooks — the app is single-tenant per instance, so a "tenant
  hook" would be a name that lies until a `tenant_id` model exists.
- **No second audit trail.** Event delivery is not durable and never claims to
  be; audit stays in `security_events` and the access log.
- **No analytics without a consumer.** The provider seam exists because the
  user asked for the capability; adding a second vendor means implementing
  `analytics.Provider`, not editing templates.

## Failure modes

| Symptom | Cause | Fix |
|---|---|---|
| `events: buffer full, event dropped` | a sink is slower than the write rate | fix the receiver, or shrink the taxonomy; the buffer is 64 |
| no webhook arrives | 3 attempts failed, or `EVENTS_WEBHOOK_URL` unset | check `events: webhook delivery failed` (attempt + status) |
| beacon 403 | RBAC row missing / signed out | the shell only loads the script for enabled analytics; the row is in `policy.csv` |
| beacon 400 | name not in the allowlist, or body over 4 KB | keep names lowercase and dotted (`todo.create`) |

Related: [configuration](configuration.md) (all switches),
[security](security.md) (audit and secrets), [async-layers](async-layers.md)
(the queue a durable outbox would use).
