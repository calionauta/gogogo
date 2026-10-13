# Admin & Dashboard

Three built-in surfaces, available as soon as the binary boots:

| Surface | URL | What it gives you |
|---|---|---|
| **Landing page** | `/` | Public marketing hero (tagline + CTA). Guests and signed-in users see the same page; no DB read, no auth gate |
| **Read-only config view** | `/config` | Auth-gated view of the running binary: env-decrypted values, masked secrets, runtime constants. Never mutates state. Source: `features/config/` |
| **Session probe** | `/api/session` | `{"authed": bool}` for the shared session banner's poll — a tab open for days outlives its cookie, and this is how the banner says so. Never 401s: a probe is a poll, not a gate |
| **Health + build tag** | `/health` | `"ok"` body (container healthcheck) + `X-Gogogo-Build` header (stale-tab reload across deploys) |
| **DagNats console** | `/console/` | Workflow runs, step inspection, JSON API for durable workflows (same-tunnel reverse proxy to the engine listener; session-gated, admin-only under RBAC) |

There is no database admin UI in the binary (no `/_/` dashboard ships
away with the old framework). Inspect data with `sqlite3` against the Turso file
(`TURSO_DB_PATH`, default `data/app.db`) — same SQLite dialect — and back it
up with `scripts/backup.sh`. For production, lock `/config` behind your
ingress (IP allowlist, oauth2-proxy in front) like any authed surface —
or set `RBAC_ENABLED=true` with `ADMIN_EMAILS` (then `/config` is
admin-only in-app; see [Configuration](configuration.md)).

## App session cookie (why one)

Login issues a single cookie: `gogogo_auth` — an opaque session token (only
its SHA-256 reaches the `sessions` table), read by `internal/auth.Session`.
HttpOnly + SameSite=Lax; `Secure` follows the environment (off in dev over
plain HTTP, on in production). Logout revokes the row; unknown and expired
sessions fail closed. There is no second cookie and no separate admin
namespace to clobber — one cookie, one session.

## DagNats console

The DagNats workflow engine exposes its own HTTP API + console at
`DAGNATS_HTTP_ADDR` (default `127.0.0.1:8090`, loopback-only — browsers
cannot reach it directly). The app mux reverse-proxies it at `/console/`
(`internal/server/console_proxy.go`): same origin and session cookie,
guests get 401, and the RBAC policy gates it admin-only. The session
email rides `X-Forwarded-User` for the console audit. Inspect runs,
steps, or trigger workflows via the API. The `WelcomeOnboarding`
workflow runs here — declarative JSON over NATS JetStream, kickstarted
automatically on first login.

In the demo deployment it is reachable at
[`/console/`](https://gogogo.calionauta.com/console/) behind the same tunnel.

## RBAC demo (viewer role)

The Casbin demo needs no new page — it reuses Todo with a second role.
`/config` shows your role and the enforcement posture (pass-through vs
enforced) for whoever looks; the viewer contract below is what the
enforcement proves:

1. Sign up a third account (leave `demo1`/`demo2` full users — their job
   is contention testing, which needs full CRUD).
2. Set `RBAC_ENABLED=true` and `VIEWER_EMAILS=<that-address>`, reboot.
3. Log in as the viewer: trash buttons render disabled with the reason,
   clicking one raises the denial message, AI Suggest hides, and forced
   deletes answer 403. Create and toggle keep working.

Viewers are todo-scoped by design (other features 403 by omission, not
by listing). Promotion is existing-rows-only and never provisions;
an address in both email lists ends `admin`. RBAC stays RBAC — there
is no ABAC surface until a consumer needs attribute policies.

## Try it live

| What | URL |
|---|---|
| **Todo & Whiteboard demo app** | [gogogo.calionauta.com](https://gogogo.calionauta.com/) |
| **Durable workflow engine (DagNats)** | [gogogo.calionauta.com/console/](https://gogogo.calionauta.com/console/) |

Log in with a seeded demo account (`demo1@demo.app` / `demo1234456`;
`demo2@demo.app` takes the same password). Anyone can sign up on the demo
— it is a demo, not a locked-down app — and the demo user from the login
placeholder is seeded on every fresh boot (`internal/demo`).

The demo runs the unified build. DagNats + NATS share a **single embedded
JetStream** on `:4222` — DagNats boots it and the whiteboard SyncWorker
attaches to it, so there is only one NATS process in the binary.

## Related

- [Configuration](configuration.md) — `DAGNATS_HTTP_ADDR` and friends.
- [Deploy](deploy.md) — putting the tunnel and ingress in front of these.