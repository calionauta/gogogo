# Architecture

This page is the map of how the pieces fit. For the per-component table with
dependencies and removal instructions, see [ARCHITECTURE.md](../ARCHITECTURE.md)
in the repo root — that file is the canonical entrypoint for LLM agents and is
kept current with the code.

## Principles

- **Unified build.** One `go build ./cmd/web` compiles everything — no build
  tags. `turso` (tursogo) is the always-on Turso driver, registered by
  `internal/db`; handlers call the generated `dbgen.*` queries directly, so
  nothing is gated behind a tag. Opt out at runtime with env vars
  (`NATS_ENABLED=false`, `DAGNATS_ENABLED=false`).
- **Features in `features/`, infrastructure in `internal/`.** Features depend
  on infra, never the reverse.
- **Wiring in `internal/server/chi.go`.** `BootChi` builds one handler per
  capability; `buildChiMux` flattens each capability's own router onto one
  chi router.
- **Startup order in `cmd/web/main.go`.**

## Directory layout

```
cmd/web/                          Entry point (flat stack + goqite + SSE Hub + DagNats + NATS)
cmd/desktop/                      Wails v3 desktop/edge shell (NATS Leaf Node when set)
cmd/gui/                          gogpu/ui native window over the same backend
config/                           Env config (+ age secrets)
internal/
  demo/                           Fresh-install demo account seed (idempotent Signup)
  secrets/                        age-decrypted secrets loader
  queue/                          goqite + SSE Hub + workers + retry + handler registry
  datastar/                       Datastar SSE rendering helpers
  nats/                           NATS JetStream + embedded server
  dagnats/                        DagNats durable workflow client
  goakt/                          GoAkt entity actors (room authority)
  llm/                            GoAI LLM SDK helpers
  collab/                         Loro CRDT + DocStore + sync workers + presence
  auth/                           Owned argon2id + sessions (gogogo_auth cookie)
  db/                             Turso connection + migrations + sqlc queries
  demo/                           Fresh-install demo account seed
  server/                         Flat-stack composition (BootChi + buildChiMux)
features/
  app/                            AppContext bundle (consumed by cmd/gui only)
  authviews/                      Login page + shell controls (theme, user menu, palette)
  landing/                        Public entry page on GET / (inside the shared shell)
  config/                         Admin-only /config view + user actions
  todo/                           Todo MVC example
  whiteboard/                     Collaborative canvas
  room/                           Room presence demo (GoAkt grains)
  notes/                          Shared plain-text notes (server-owned Loro Text)
  sounds/                         UI sound feedback (cuelume)
web/
  resources/                      Embedded static assets (AssetHandler: /static/ + root /sw.js)
```

The annotated version of this tree, with SCOPE markers on every entry, is in
[Scope taxonomy](scope-taxonomy.md).

## Dependency direction

```
cmd/web  ──▶  internal/server  ──▶  features/*/chi  ──▶  internal/*  ──▶  (chi, stdlib, deps)
                       │                    │
                       └────────────────────┘
         features depend on internal, never the reverse
```

`internal/server/chi.go` is the single place where the dependency graph is
assembled. If you are adding a feature, that is the only file you touch for
wiring — the feature's own `Routes()` does the rest.

## The three app entry points

One backend, three frontends:

| Entry point | Transport | Purpose |
|---|---|---|
| `cmd/web` | HTTP + SSE | The main server: router, queue, realtime |
| `cmd/desktop` | Wails v3 webview over a local HTTP proxy | Native desktop/edge shell, 100% shared backend |
| `cmd/gui` | gogpu/ui native window | Second frontend with **no** HTTP at all — proves the domain layer is transport-agnostic |

`cmd/desktop` and `cmd/gui` are separate build targets excluded from the web
CI. See [Desktop & Mobile](desktop-mobile.md).

## Route wiring

The flat stack composes one `chi.Router` (`buildChiMux` in
`internal/server/chi.go`). Each capability exposes `Routes() chi.Router` with
absolute patterns, and `MountFeature` flattens that table onto the root
(`internal/server/route.go`). Therefore:

- One `MountFeature` call per capability. A route added to a feature is
  reachable without an edit at the root, and a duplicate method+pattern is a
  boot panic that names both owners.
- The auth API is a feature like any other: `internal/auth` exports
  `Routes() chi.Router` with absolute patterns and the router decides the
  method. Static/asset handlers mount through `MountSubtree`, which leaves
  the request path unrewritten because those handlers trim their own prefix.
  The landing router is the not-found handler.
- Serve static assets through `web/resources.AssetHandler` (`/static/` plus
  root `/sw.js` with `Service-Worker-Allowed: /`).
- The app cookie is `gogogo_auth` — the only cookie — see
  [Admin & Dashboard](admin-dashboard.md#app-session-cookie-why-one).

### Why not chi's Route/Group

`Route` is prefix-scoped. Six of the eleven feature packages own more than one
top-level prefix: `todo` answers `/todo`, `/api/todos`, `/login` and `/logout`;
`whiteboard` answers `/whiteboard` and `/api/whiteboard`. With `Route`, each
feature would need two to four mount calls plus a split `Routes()` API, and the
root would own the prefix while the feature owned the suffix. The current
shape keeps one fact in one place: the feature owns the whole path, and the
root only decides that it is mounted.

`Group` and `With` scope middleware without a prefix. The cross-cutting layers
here are applied above the router on purpose: RBAC must deny before route
resolution (a 403 for an unknown path, not a 404 that answers "does this route
exist?"), and the security headers and the access log must cover the RBAC 403
and the router 404 alike. Recovery is the one middleware inside the tree,
because it must sit next to the handler it protects.

### Middleware and SSE

A wrapper that hides `http.Flusher` turns every SSE handler into a 500. The
opt-in access log uses chi's `middleware.NewWrapResponseWriter`, which forwards
`Flusher`, `Hijacker` and `Pusher`; `TestAccessLogPreservesFlusher` fails if
that stops being true. The same reason rules out a global timeout or
compression middleware: both buffer or cut a response that never ends.
`http.Server.WriteTimeout` stays 0 for the same reason.

### Why this is easy for an agent to hold

- Every path is written once, as an absolute literal in the feature that owns
  it. `grep 'r.Get("/api/notes'` finds the route and nothing has to be
  resolved against a prefix table.
- `docs/api-routes.md` and `docs/unit-contracts.md` are generated from the
  same source, so the inventory cannot drift from the router.
- A duplicate claim names both owners at boot; a missing method answers 405 at
  the router; an unknown path is the landing router's 404. There is no
  hand-rolled `if r.Method !=` left in the auth surface.
- The RBAC scan reads the same `r.Get/Post` lines, so a new endpoint is either
  covered or red on the first commit — including `internal/auth`, which the
  scanner used to miss entirely.

### Scaling the route table

One line per capability does not get worse with size: boot walks the routers
once and registers one trie, and adding a feature adds one line. A team can add
routes inside its own feature without touching the root.

If a subset ever needs its own middleware (rate limiting under `/api`, say),
add it where it is scoped: a mount helper that takes a middleware chain, or a
`Group` on `Router` that keeps the same route table. Neither exists yet; both
are additive and leave the features' absolute paths untouched, so the decision
can wait for the second caller. That is the YAGNI rule the rest of the stack
follows.

### Security boundary

chi is a router, not a security layer. Its middleware package has no auth, CSRF
or security headers; `RealIP` trusts a client-controlled header, and `Throttle`
is per-process. The security properties here are owned:

- argon2id passwords, opaque sessions, a `SameSite=Lax` + `HttpOnly` cookie
  that is `Secure` in production (`internal/auth`).
- CSRF in layers: the cookie's `SameSite=Lax` keeps it off cross-site
  POSTs; `sameOriginGuard` refuses an unsafe method when
  `Sec-Fetch-Site: cross-site` or a browser `Origin` does not match the
  request host; magic links confirm on a POST (cross-device by default;
  strict same-browser is opt-in) and the OAuth handshake is bound to the
  requesting browser by a short-lived cookie. A reverse proxy
  in front must preserve the `Host` header (cloudflared and Caddy do); a
  proxy that rewrites it makes every browser write a 403. CLI clients,
  webhooks and the desktop webview send no `Origin` (or a custom scheme) and
  pass; two loopback origins share the dev trust boundary.
- Casbin policy at the edge of the router plus per-handler session checks,
  because `RBAC_ENABLED` defaults to true in production (false in development)
  (`docs/rbac.md`). Operator surfaces
  (`/config`, `/console/`, `/dagnats/`) carry an always-on fence that does not
  depend on the flag: admin role for `/config`, and admin **plus** an
  `ADMIN_EMAILS` match for the console (the public demo's `demo1` is an admin,
  so "is admin" alone is not an operator identity).
- templ escaping and sqlc parameters on the data paths.
- a per-request CSP nonce (`script-src 'self' 'nonce-…'`), with the nonce on
  `<html data-nonce>` for Datastar's expression evaluator; plus
  `X-Content-Type-Options`, `Referrer-Policy`, `X-Frame-Options`, COOP/CORP,
  `Permissions-Policy`, and HSTS outside dev. `Cache-Control: no-store` on
  every auth handler response, plus a body cap on the auth surface.
- magic links are built only from `APP_URL`, never from the request `Host`;
  the emailed GET is read-only and the POST confirms the account. Cross-device
  works by default; `MAGIC_LINK_BIND_BROWSER=true` is the strict same-browser
  mode. The OAuth handshake carries a browser-binding cookie.
- public signup is closed by default in production; the demo seed is
  development-only unless `DEMO_ACCOUNTS_ENABLED=true`, and an existing demo
  pair is disabled on boot when the flag is off. Signup, magic-link
  requests and OAuth starts carry per-IP budgets, and signin/TOTP keep their
  own; the per-session limiter covers everything authenticated.
- sign-out-everywhere uses the password when set, a current authenticator/
  recovery code when TOTP is enrolled, or the session when the account has no
  factor; TOTP disable and recovery rotation require the password or a code on
  OAuth-only rows. Admin role changes and
  session revocation require the admin's own password when set, or a current code on a passwordless admin.
- `security_events` is the durable audit twin of the security log: subject,
  actor and outcome only, 30-day retention pruned at boot and daily in
  1000-row batches. `/config` renders the 24-hour aggregates, the filtered
  rows and a CSV; browser CSP violations post to `/api/security/csp-report`
  with the query stripped before logging. The full map is
  [Security model](security.md).

The edge reverse proxy still owns TLS, WAF and fleet-wide rate limiting, and
the in-process limiter keys on the **session**, never the client IP (behind a
tunnel every request arrives from the same proxy address). The sign-in
throttle deliberately keys on the TCP peer; behind a single proxy that
collapses to one key, so throttle login at the edge. These are documented
boundaries, not silent assumptions.

Mechanical guards behind that boundary: `TestHandlerSurfaceHasNoRawSinks`
scans every `features/*/handlers` and `internal/server` file for `http.Get`,
`exec.Command`, raw `os.Open`/`os.WriteFile`, `templ.Raw` and
`template.HTML`; `TestRBACPrivateRoutesDenyAnonymous` refuses a policy that
publishes an undeclared route; `TestPrivatePagesRejectAnonymous` proves the
handler session check with RBAC off; auth/sanitizer fuzz seeds cover the pure
parsers.

Residual hardening deliberately left out of the audit pass: an idle session
timeout (the template keeps the absolute seven-day TTL), the `__Host-` cookie
prefix (it would rename the cookie and invalidate every session on deploy),
TOTP last-step replay tracking (needs a schema column for a 30-second window),
and breached-password checks (an external-service dependency). Each is a
product or schema decision, not a missing primitive.

## SCOPE annotations

Every non-test, non-generated `.go` file under `internal/` and `features/`
carries a leading doc-comment line declaring its removal class:

```
// SCOPE:<core|plugin|feature> — <short description>
```

The directory is the layer (`internal/` = infra, `features/` = product); the
annotation only records what happens if the file is deleted. See
[scope-taxonomy](scope-taxonomy.md).

The `cmd/check-scope` program walks every `.go` file in `internal/` and
`features/` and asserts the canonical line is present. `make ci-local` runs it,
and the lefthook pre-commit runs it whenever staged files match
`{internal,features}/**/*.go`. Migrating an existing file uses
`python3 scripts/migrate-scope.py` (idempotent).

See [Scope taxonomy](scope-taxonomy.md) for the full rules.

> **Native boundary (not a layer).** `native` is a reserved implementation
> boundary, not a fourth SCOPE value. There is no Zig code or toolchain in
> this repo. If a justified case ever arrives, the Go API lives in its normal
> package with the usual SCOPE and Zig sources stay inside it behind a small
> C ABI — see [Native boundary](exception-to-go.md). Do not create a top-level
> `native/` directory speculatively.

## Related

- [Stack in layers, not silos](stack-layers.md) — what each dependency is for.
- [Seven async layers](async-layers.md) — the realtime and async topology.
- [Configuration](configuration.md) — where constants live and why.
- [Native boundary](exception-to-go.md) — Go-first policy and the Zig escape hatch.