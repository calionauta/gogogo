# Admin & Dashboard

The built-in surfaces, available as soon as the binary boots:

| Surface | URL | What it gives you |
|---|---|---|
| **Landing page** | `/` | Public entry page inside the shared shell (demo CTA). Guests and signed-in users see the same page; no DB read, no auth gate |
| **Config + admin users** | `/config` | Admin-only view of the running binary (masked secrets) plus user role changes and session revocation. Each mutation requires the admin's password when set, or a current authenticator/recovery code on a passwordless admin; the fence is always on, independent of `RBAC_ENABLED`. Source: `features/config/` |
| **Session stream** | `/api/session/stream` | `data: {"type":"session","authed": bool, "unread": number}` frames for the shared session banner and the rail's inbox badge. One VISIBLE tab per browser holds this stream (Web Locks + BroadcastChannel); going hidden hands the lock on. One frame on connect and one only when the state changes (session expiry or unread count); the server closes after `authed:false` so the client stops reconnecting. Without Web Locks + BroadcastChannel, pages that already hold a feature stream open their own (the fallback that keeps the connection budget). Never 401s: this reports a state, it is not a gate |
| **Health + build tag** | `/health` | `"ok"` body (container healthcheck) + `X-Gogogo-Build` header (stale-tab reload across deploys) |
| **DagNats console** | `/console/` | Workflow runs, step inspection, JSON API for durable workflows (same-tunnel reverse proxy to the engine listener; admin **and** `ADMIN_EMAILS`-listed, always on — an empty list closes it) |

The map for these controls is [Security model](security.md).

`/config` also carries the **security events** panel: the last 24 hours
grouped by event/reason, a warning when rejected authentication crosses the
alert threshold, and the most recent 100 rows from `security_events`
(30-day retention, pruned at boot and daily in 1000-row batches). Admin
mutations (role change, session revoke) record both the target and the
operator; the table links to `/config/audit.csv` for an incident review. The
panel filters by event and pages through older rows; the CSV applies the same
filter.

Events never contain a password, session token, TOTP code or magic-link
token, and a reported CSP URL drops its query string so a one-time token
cannot ride into the log. Browser CSP violations post to
`/api/security/csp-report` and are logged, so a blocked inline script leaves a
trace instead of an empty console.

There is no database admin UI in the binary (no `/_/` dashboard ships
away with the old framework). Inspect data with `sqlite3` against the Turso file
(`TURSO_DB_PATH`, default `data/app.db`) — same SQLite dialect — and back it
up with `scripts/backup.sh`. `/config` is admin-only in the handler even
with `RBAC_ENABLED=false`; for a real deployment still put an IP allowlist
or `oauth2-proxy` in front of it, like any operator surface. The console
adds a second fence: the session address must be in `ADMIN_EMAILS`, so the
published demo admin cannot reach it.

## App session cookie (why one)

Login issues a single cookie: `gogogo_auth` — an opaque session token (only
its SHA-256 reaches the `sessions` table), read by `internal/auth.Session`.
HttpOnly + SameSite=Lax; `Secure` follows the environment (off in dev over
plain HTTP, on in production). Logout revokes the row; unknown and expired
sessions fail closed. There is no second cookie and no separate admin
namespace to clobber — one cookie, one session.

Credential lifecycle is also explicit over the API, for clients that never
render the page: `POST /api/auth/password` (current + new password)
rotates the hash, **revokes every session**, and issues a fresh cookie for
the caller; `POST /api/auth/sessions/revoke-all` signs out everywhere and
uses the password when set, a current authenticator/recovery code when TOTP is
enrolled, and the session only for an OAuth-only account with no factor.
The authenticated `/account` page is the user-facing home for email
verification, password change, TOTP enrollment/recovery and session
revocation. Rotating recovery codes and disabling the factor both require the
current password when the account has one, or a current authenticator/recovery
code on an OAuth-only row. The session list shows a browser/OS hint captured
from the User-Agent (the raw string never leaves the owner's page), the short
opaque id, and a "this device" badge; the full id stays in the row's title. `GET /api/auth/sessions` lists the caller's sessions with opaque ids;
`DELETE /api/auth/sessions/{id}` revokes one. `POST /api/auth/verify/request`
sends the verification link, `POST /api/auth/password/reset/request` sends a
reset link, and
`/api/auth/password/reset?token=…` is the set-a-new-password page. Reset
consumes the token, rotates the password and revokes every session. Disabling
TOTP uses the same revocation, so a stolen cookie stops working the moment the
factor is removed. With `SESSION_IDLE_TIMEOUT` set, an inactive session ends
before the absolute seven-day TTL.

## DagNats console

The DagNats workflow engine exposes its own HTTP API + console at
`DAGNATS_HTTP_ADDR` (default `127.0.0.1:8090`, loopback-only — browsers
cannot reach it directly). The app mux reverse-proxies it at `/console/`
(`internal/server/console_proxy.go`): same origin and session cookie,
guests get 401, and two fences run before the proxy: the session must be an
admin **and** its address must be listed in `ADMIN_EMAILS`. Both are
always on, so `RBAC_ENABLED=false` does not expose the console; an empty
`ADMIN_EMAILS` closes it entirely. The session email rides
`X-Forwarded-User` for the console audit. Inspect runs,
steps, or trigger workflows via the API. The `WelcomeOnboarding`
workflow runs here — declarative JSON over NATS JetStream, kickstarted
automatically on first login.

On a deployment that lists its operator in `ADMIN_EMAILS` it is reachable
at `/console/` behind the same tunnel; the branded alias `/dagnats/`
re-roots onto the same tree. The public demo does not list one, so the
console is closed there.

## RBAC demo (viewer role)

The Casbin demo needs no new page — it reuses the surfaces with two roles,
and the demo pair is seeded with them **when `DEMO_ACCOUNTS_ENABLED=true`**
(the development default): `demo1` boots as `admin`, `demo2` as `viewer`
(`internal/demo`, rewritten every boot). With the flag off, an existing pair
is disabled on boot (unusable password, plain user role, sessions revoked). A production deploy must opt in;
otherwise neither account exists. You do not need a third account or
`VIEWER_EMAILS` to see enforcement when the seed is on.

`/config` shows your role and the posture (pass-through vs enforced);
`RBAC_ENABLED` defaults to **true in production** (false in development), so a
deployed binary is enforced unless an operator opts out explicitly. Under
enforcement the two roles show the contract:

| | demo1 (admin) | demo2 (viewer) |
|---|---|---|
| Todos | everything | read, create, toggle, delete-own |
| Notes / whiteboards | open, **edit any**, delete any | open, **edit any**, delete **own only** |
| AI Suggest, `/config` | yes | 403 |
| `/console` | only with an `ADMIN_EMAILS` match | 403 |

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

Log in with a seeded demo account when the deployment enables the demo
seed: `demo1@demo.app` / `demo1234456`; `demo2@demo.app` takes the same
password. `SIGNUP_ENABLED` and `DEMO_ACCOUNTS_ENABLED` default to off in
production; a public demo sets both explicitly. See
[Configuration](configuration.md).

The demo runs the unified build. DagNats + NATS share a **single embedded
JetStream** on `:4222` — DagNats boots it and the whiteboard SyncWorker
attaches to it, so there is only one NATS process in the binary.

## Related

- [Configuration](configuration.md) — `DAGNATS_HTTP_ADDR` and friends.
- [Deploy](deploy.md) — putting the tunnel and ingress in front of these.