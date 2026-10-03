# Admin & Dashboard

Three built-in surfaces, available as soon as the binary boots:

| Surface | URL | What it gives you |
|---|---|---|
| **Landing page** | `/` | Public marketing hero (tagline + CTA). Guests and signed-in users see the same page; no DB read, no auth gate |
| **Read-only config view** | `/config` | Auth-gated view of the running binary: env-decrypted values, masked secrets, runtime constants. Never mutates state. Source: `features/config/` |
| **PocketBase admin** | `/_/` | Data browser, REST playground, superuser management, backups, logs |
| **DagNats console** | `:8090` / `/dagnats/` | Workflow runs, step inspection, JSON API for durable workflows |

## PocketBase admin UI (`/_/`)

- **Visual data browser** for every collection (todos, users, …) with
  sort/filter/CSV export
- **REST + JS SDK playground** for the endpoints PocketBase generated from your
  schema
- **Superuser management** (create the first one via the install link printed in
  the server logs)
- **File storage** (S3-compatible uploads, images, attachments)
- **Backups** (SQLite snapshot, download + restore)
- **Logs** (requests, errors, slow queries)

This is **not** a custom admin panel — it's the upstream PocketBase UI,
embedded in the same binary on the same port. No extra service to deploy, no
extra auth to wire. For production, point a Cloudflare Tunnel / Caddy ingress
at the same `/_/` path and lock it down (IP allowlist, oauth2-proxy in front,
or just PocketBase's own superuser auth).

## App session cookie vs `pb_auth` (why two)

The app **never** reuses PocketBase's own `pb_auth` cookie. PocketBase keeps
the superuser (`_superusers`) and regular users as **separate auth namespaces
with different endpoints**, and a single client holds only **one** auth state
(one cookie). Sharing `pb_auth` for the app session clobbers the admin session
in the same browser — and vice versa. This is a well-known PocketBase gotcha
([#5050](https://github.com/pocketbase/pocketbase/issues/5050),
[#1780](https://github.com/pocketbase/pocketbase/issues/1780)).

So login issues **two** cookies:

- `gogogo_auth` — the app's own session cookie, read by `LoadAuthFromCookie`.
- `pb_auth` — the same token under PocketBase's native name, so PB-native
  surfaces (notably the `/api/realtime` SSE channel for record-change
  subscriptions) authenticate as the same user. Without it, realtime record
  events are silently dropped by PB's per-subscriber access check.

The split is **intentional, not tech debt** — keep the two cookies separate.

**Best practice:** run the admin UI on a separate origin/port (e.g. `:8090/_/`)
so even `pb_auth` never collides between admin and app.

## DagNats console

The DagNats workflow engine exposes its own HTTP API + console at
`DAGNATS_HTTP_ADDR` (default `127.0.0.1:8090`). Inspect runs, steps, or trigger
workflows via the API. The `WelcomeOnboarding` workflow runs here —
declarative JSON over NATS JetStream, kickstarted automatically on first login.

In the demo deployment it is reachable at
[`/dagnats/`](https://gogogo.calionauta.com/dagnats/) behind the same tunnel.

## Try it live

| What | URL |
|---|---|
| **Todo & Whiteboard demo app** | [gogogo.calionauta.com](https://gogogo.calionauta.com/) |
| **Live PocketBase admin dashboard** | [gogogo.calionauta.com/_/](https://gogogo.calionauta.com/_/) |
| **Durable workflow engine (DagNats)** | [gogogo.calionauta.com/dagnats/](https://gogogo.calionauta.com/dagnats/) |

Log in with the seeded demo account (`demo@demo.app` / `demo`). The demo's
`users` collection is **locked** — visitors can log in as the demo user but
cannot create or delete accounts through the API or the dashboard (only the
superuser can).

The demo runs the unified build. DagNats + NATS share a **single embedded
JetStream** on `:4222` — DagNats boots it and the whiteboard SyncWorker
attaches to it, so there is only one NATS process in the binary.

## Related

- [Configuration](configuration.md) — `DAGNATS_HTTP_ADDR` and friends.
- [Deploy](deploy.md) — putting the tunnel and ingress in front of these.