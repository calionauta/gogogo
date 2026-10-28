# Notifications: durable delivery with a local inbox

The inbox is the template's delivery slice: a message that reaches a person at
a time the app chose, with a record of whether it arrived. It runs on the
DagNats engine the binary already hosts, so the delay survives a restart and
holds no goroutine while it waits.

The provider half is deliberately absent. This page describes the local
implementation; the comparison against an external delivery engine and the
seam that would host one live in [notify](research/notify-2026-10.md).

## What ships

| Route | Method | Purpose |
|---|---|---|
| `/notifications` | GET | The inbox page: unread count, rows, reminder form |
| `/api/notifications/stream` | GET | Datastar SSE: ack, initial inbox, then one patch per change |
| `/api/notifications/badge` | GET | One Datastar signal patch with the unread count (the shell's refresh on visibility/focus/online) |
| `/api/notifications/remind` | POST | Schedule one reminder (`text`, `delay` of `1m`/`1h`/`24h`); answers with the inbox patch plus a toast |
| `/api/notifications/{id}/read` | POST | Mark one row read; patches `#inbox` and the rail count |
| `/api/notifications/read-all` | POST | Mark every in-app row read; patches `#inbox` and the rail count |

Every route checks the session itself. RBAC can be off, so a policy row is
never the only guard, and every query is scoped by the session owner.

## The rail indicator

The sidebar's Inbox entry carries the unread count as a DaisyUI `indicator`
bubble on its icon, above the first group heading. Three writers keep it honest:

- `inboxBadgeMiddleware` (`internal/server/inbox_badge.go`) resolves the count
  once per page navigation and seeds the shell signal, so every page renders
  the rail with the right number without importing the delivery store.
- The inbox stream and both read actions merge `$inboxUnread` together with
  the `#inbox` patch (`patchInboxData`), so reading a row on the inbox page
  lowers the rail count in the same frame.
- The browser's leader session stream (`/api/session/stream`, opened by
  `session.js` on pages with no feature stream and shared through a
  BroadcastChannel) carries an `unread` field on its session frame. Pages that
  already hold a feature stream must not open a second SSE connection — the
  browser's six-per-host cap already fills at five same-user tabs — so the
  shell's hidden button refetches `/api/notifications/badge` on visibility,
  focus, bfcache restore and reconnect — no timer. The endpoint answers with a
  signal patch, not markup. Any tab that learns a new count broadcasts it over the
  BroadcastChannel, so a read in the inbox tab reaches the other tabs at once.

The badge node always exists (hidden at zero) and its text/visibility are bound
to `$inboxUnread`: the server-rendered value is the pre-init fallback. The
notifications bus is per-process; the stream's 60-second session recheck is the
fallback that catches a delivery handled by another instance, and the docs say
so rather than promising an instant cross-instance wire that is not built.

Scheduling answers with a **toast** ("Reminder scheduled.") appended to the
shell's `#toast-container`, not an inline notice: the result is transient, the
list below is the evidence, and a notice would stay on the page until the next
navigation. The rule is in `skills/gogogo-ui/SKILL.md` and
[UI compositions](ui-compositions.md#7-transient-feedback-toast).

## The durable path

Scheduling starts the `reminder` workflow (`internal/dagnats/reminder.go`):

1. `seed` — a normal task that forwards the run input as its output. It exists
   because of how the engine resolves input: a step with one dependency receives
   that dependency's **output**, and a sleep step produces none. Seeding the
   payload first is what carries it across the wait.
2. `wait` — a `sleep` step whose `until_input_path` is the run's `due_at`
   (RFC3339). The engine owns the timer, so a restart resumes the run instead
   of losing it.
3. `deliver` — the `notification-deliver` worker depends on both `seed` and
   `wait`, so its input is `{"seed": {…}, "wait": null}`; it writes one row
   through the feature's `Deliver`.

Two dedupe guards, on purpose: the workflow's `idempotency_key` stops a double
submit from starting two runs, and the `notifications_txn` unique index stops a
retried step from writing two rows. The engine only dedups run starts.

`(owner, transaction_id, channel)` is unique. A duplicate write returns
`sql.ErrNoRows`, which the deliverer treats as success — the user already has
the notification — and publishes no event, so a retry cannot make a tab flicker.

**One delivery path, two producers.** The onboarding workflow calls the same
`Deliver` when it finalizes, so "tell the user" has one implementation instead
of one per workflow. A third producer is a `Delivery` value, not a new table.

## The live inbox

`/api/notifications/stream` is a Datastar SSE stream, opened by the shared
`components.StreamOpener`. It sends the open ack first, then the inbox, then
sleeps until the feature bus says the owner's inbox changed. The bus
(`features/notifications/bus.go`) is in-process: the delivery worker publishes
an owner-scoped event, and every open tab of that owner re-renders `#inbox`.

Two details that keep it honest:

- The event carries `Except`, the clientID of the tab whose own request already
  rendered the change. That tab is skipped, so a mark-read click does not get a
  second, identical patch from its own stream.
- The bus is **local to one process**. A second instance's tabs reconnect to
  their own instance and re-read on open, so the inbox is correct without a
  cross-instance wire. Wiring NATS (as the todo bus does) is the change to make
  when a deployment runs several instances with a stream per user; it is not
  built before it is needed.
- The session is re-checked on every heartbeat. A revoked session closes the
  stream; the reconnect gets a 401, which the shell turns into a login
  redirect.

## Why the row keeps "skipped"

`status` (`sent`, `error`, `skipped`, `throttled`) plus `skip_reason` means a
delivery that did **not** happen is a record, not silence. That is what the
queue and the SMTP mailer cannot express today: they retry and forget.

## Why DagNats and not the queue

goqite is the right tool for a job that runs once, in the background, and
reports through the SSE hub: fetches, AI calls, retries. It is the wrong tool
for "do this at 09:00 in the user's timezone", because a delayed row is an
implementation detail you would have to keep alive across restarts and upgrades
yourself. The engine owns that timer, the run state, and the retry policy; the
queue keeps the work that is *now*, not *later*.

## Transport: channels and providers

The inbox is local; external delivery is a channel that a provider carries.
`internal/delivery` is the seam — one `Sender` (`Name` + `Send`) — and the
feature never imports a provider package.

A **channel** is the medium the person perceives (`email`, `sms`, `chat`,
`push`); a **provider** is who carries it (`smtp`, `notify`, later `telegram`).
That split is why the router is keyed by channel and a `Sender` names its
provider: swapping the provider does not change the feature, the row or the
form.

| `DELIVERY_TRANSPORT` | Provider | What it does |
|---|---|---|
| `off` (default) | — | Nothing external is sent. A requested channel with no transport is recorded `skipped`, `skip_reason=provider`. |
| `smtp` | in-process | Reuses the auth stack's SMTP implementation (`SMTP_ADDR`/`SMTP_FROM`); the adapter takes the mailer as a function, so `internal/delivery` never imports `internal/auth`. |
| `notify` | external | `internal/delivery/notify` triggers `gogogo-delivery` on an external [notify](https://github.com/danmestas/notify) engine over `POST /v1/events/trigger`, carrying the same transaction id, so a retried send is deduped on both sides. |

`in_app` is deliberately **not** a provider channel: the inbox row is written
by the feature, always, before the loop. A provider that could own `in_app`
would create a second owner of the inbox.

The notify side needs one workflow and one integration before the switch is
meaningful:

```bash
curl -X POST localhost:3000/v1/workflows \
  -H "Authorization: ApiKey $KEY" -H 'Content-Type: application/json' \
  -d '{"name":"GoGoGo delivery","triggerIdentifier":"gogogo-delivery",
       "steps":[{"stepId":"email","type":"email",
                 "controlValues":{"subject":"{{payload.subject}}","body":"{{payload.body}}"}}]}'
```

**Adding a channel is not a gogogo task.** The template ships two: `in_app`
(written locally) and `email` (the one external channel it configures, reusing
the SMTP relay auth already needs for verification and reset). Any other
channel — sms, chat, push — belongs to the external service: configure it
there, and the message carries the channel as data. The app keeps no address
book beyond `users.email`, and does not grow a `DELIVERY_<CHANNEL>` surface.

The rule: **gogogo owns the inbox and the email address it already has;
everything else the provider owns.** That is why a channel is a string, not an
enum of everything the world supports: `sms` is legal data that reaches a
provider which knows it, or is recorded `skipped/unsupported`.

**What the row means.** An external channel goes `pending` at write time, then
`sent` (the provider accepted the send), `error` (with `error_text`), or
`skipped` (`provider` when no transport is configured, `no_address` when the
user has no address for that channel). The provider sees
`<transaction>:<channel>` as its transaction id, so a provider that dedupes on
transaction+recipient (notify does) never treats the second channel of one
delivery as a retry. Acceptance is not arrival: notify delivers
asynchronously, so its own message record is the deeper evidence. An external
failure never fails the workflow step — the in-app row is the contract and the
user already has the message.

**Why the inbox list stays `in_app`-only.** The email row is the same table, one
channel per row, so the audit trail is complete without the page showing a
second copy of what the user already read.

The library survey behind this model (shoutrrr, notify, gopackx, thinwrap,
apprise-go) is in [notify → the channel model](research/notify-2026-10.md#the-channel-model-and-the-libraries-that-agree).

## Degrade paths

- `DAGNATS_ENABLED=false`: the page still renders the inbox; the reminder form
  is replaced by a notice, because a durable delay needs the engine. The stream
  keeps working: deliveries from other producers still arrive.
- An unknown `delay` is a `400`, never a default. A reminder that fires at a
  time the user did not pick is worse than an error.
- The inbox lists the newest 50 rows. A template demo does not paginate; the
  count is a separate index lookup so the badge stays cheap.
- The service worker skips this stream (see `SKIP_PATTERNS` in `sw.js`); an
  intercepted `EventSource` would never deliver.

## What is not here

SMS, push, chat providers, digests, preference opt-outs, and a cross-instance
bus. The [notify evaluation](research/notify-2026-10.md) records why: the inbox
stays local, delivery is a per-channel seam, and an external engine is
transport, never the owner of `in_app`. A new provider is one `Sender` plus one
`DELIVERY_*` value, not a change to the gate.

## Removing it

Manual, because the slice is wired into a few places by design:

1. Delete `features/notifications/`.
2. Delete the sidebar entry in `internal/ui/modules.go` and its icon in
   `internal/icon/icons.go`.
3. Delete `internal/dagnats/reminder.go`, the `notification-deliver` and
   `reminder-seed` workers, the onboarding welcome call, and the `startDagNats`
   argument in `cmd/web/dagnats.go`, plus the call-site argument in
   `cmd/web/main.go`.
4. Remove the `notifications` entry from `internal/capabilities/registry.go`
   and the policy rows from `internal/rbac/casbin/policy.csv`.
5. Leave migration `0015_notifications.sql` in place: migrations are
   append-only. Drop the table with a new migration if the data must go.
