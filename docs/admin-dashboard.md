# Admin & Dashboard

Three built-in surfaces, available as soon as the binary boots:

| Surface | URL | What it gives you |
|---|---|---|
| **Landing page** | `/` | Public marketing hero (tagline + CTA). Guests and signed-in users see the same page; no DB read, no auth gate |
| **Read-only config view** | `/config` | Auth-gated view of the running binary: env-decrypted values, masked secrets, runtime constants. Never mutates state. Source: `features/config/` |
| **Session stream** | `/api/session/stream` | `data: {"type":"session","authed": bool}` frames for the shared session banner — a tab open for days outlives its cookie, and this is how the banner says so. One VISIBLE tab per browser holds the stream (Web Locks + BroadcastChannel); going hidden hands the lock on, and the others update from its message. One frame on connect and one only when the state changes; the server closes after `authed:false` so the client stops reconnecting. Never 401s: this reports a state, it is not a gate |
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
[`/console/`](https://gogogo.calionauta.com/console/) behind the same tunnel
(the topbar links the branded alias
[`/dagnats/`](https://gogogo.calionauta.com/dagnats/), which re-roots onto
the same tree).

## RBAC demo (viewer role)

The Casbin demo needs no new page — it reuses the surfaces with two roles,
and **the demo pair is already seeded with them**: `demo1` boots as
`admin`, `demo2` as `viewer` (`internal/demo`, rewritten every boot). You
do not need a third account or `VIEWER_EMAILS` to see enforcement.

`/config` shows your role and the posture (pass-through vs enforced);
`RBAC_ENABLED` defaults to **false**, so enforcement only appears once it
is set. Under enforcement the two roles show the contract:

| | demo1 (admin) | demo2 (viewer) |
|---|---|---|
| Todos | everything | read, create, toggle, delete-own |
| Notes / whiteboards | open, **edit any**, delete any | open, **edit any**, delete **own only** |
| AI Suggest, `/config`, `/console` | yes | 403 |

The middle row is why the collab demo exists: sharing means *editing* is
open to every login (that is what proves live collaboration from two
browsers), while *deleting is the row's author or an admin*. A viewer is
refused a foreign delete with 403 and keeps the edit right. That rule is
`canDeleteDoc` in each feature's handler, not in this policy — the policy
grants the verb class, the handler decides the row.

Outside those three surfaces a viewer is denied by omission, not by
listing: the todo surface and shared docs are enumerated in
`casbin/policy.csv`, everything else 403s. Promotion via
`ADMIN_EMAILS`/`VIEWER_EMAILS` is existing-rows-only and never
provisions; an address in both lists ends `admin`.

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