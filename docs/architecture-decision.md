# Architecture decision: flat stack over framework + abstraction

Status: accepted (2026-10-11)

## Decision

gogogo targets an explicit, flat stack: the **Go standard library mux**
(`net/http`, composed one subtree per capability) for routing,
**sqlc-generated functions called directly from handlers** for data access,
**Turso Database** (the Rust SQLite-compatible engine, driven by
`turso.tech/database/tursogo`), **owned argon2id auth** (`internal/auth`:
password hashes, opaque sessions, `gogogo_auth` cookie, zero auth
dependencies), **Casbin** for permissions, and **Datastar + Templ** for the UI.
PocketBase, the limen auth library and the repo-interface abstraction layer
(`internal/ports`, `internal/repos`, `internal/backend`) are removed.

## Why (evidence)

The thesis is one sentence: **few deep modules beat many shallow ones.** The
benefits, in the order they show up in daily work:

- 🧩 **No shallow middle layer.** The removed layer was three tiny interfaces
  with two one-line adapters each — files and indirection, no behavior. A
  pass-through that only forwards a call is a cost with no return.
- ⚡ **Errors arrive at compile time.** Calling `dbgen.CreateTodo` directly turns
  a wrong query name, a missing argument, or a changed column into a build
  failure instead of an interface mismatch discovered at runtime — the tight
  feedback loop AI coding depends on.
- 🗄️ **The database is a file.** Embedded SQLite through sqlc-generated queries:
  no server to install, no connection string to rotate, no ORM model language,
  no strategy switch between data layers.
- 🔑 **Auth is owned code.** Password hashes, opaque sessions, and the
  `gogogo_auth` cookie are a few hundred lines in `internal/auth` — zero auth
  dependencies, and random session tokens mean there is nothing to rotate.
- 🚀 **One artifact, one deploy.** No framework runtime, no orchestrator, no
  configuration living in a dashboard.
- 🧠 **Fewer concepts to hold.** A reader — agent or new teammate — follows
  `handler → query → table` and is done. There is no framework chapter to
  internalize before the first line of product code.

> "Bad codebases have lots of shallow modules with big interfaces. Good
> codebases have a few big modules with simple interfaces."
> — Matt Pocock, *9 Ways AI Coding Has Rewired My Brain* (2026)

**Counterpoint, kept honest.** An abstraction is justified when there are
genuinely interchangeable implementations — swapping LLM providers is the real
case. `internal/llm` stays a (deep) module for exactly that reason. The rule is
not "no interfaces"; it is "no interfaces with one implementation and no
behavior."

**The cost is real, and it lives elsewhere.** Owning the commodity parts, a
pre-1.0 engine, and the PocketBase rejection are stated plainly in
[The flat stack — the cost](stack-tradeoffs.md#the-flat-stack-the-cost) and
[Turso Database: the cost, stated plainly](stack-tradeoffs.md#turso-database-the-cost-stated-plainly).

## Target directory structure

| Concern | Location |
|---|---|
| HTTP handlers (std mux) | `features/<name>/handler.go` |
| DB connection, pragmas, boot | `internal/db/db.go` |
| Schema (the migrations ARE the schema) | `internal/dbschema/migrations/*.sql` |
| sqlc query sources | `internal/db/queries/<domain>.sql` |
| sqlc config | `internal/db/sqlc.yaml` |
| sqlc-generated Go (`package dbgen`) | `internal/db/sqlcgen/` |
| Auth (owned, argon2id + sessions) | `internal/auth/` |
| Casbin model + policy (embedded) | `internal/rbac/casbin/` |
| RBAC middleware | `internal/rbac/` |
| Business logic | `features/<name>/` |

Everything data-shaped lives under one tree, `internal/db/`, so there is exactly
one connection string, one schema, and one generated package to reason about.
PocketBase's old `internal/db/` package is deleted with it, which frees the top-level
`internal/db/` name; promoting `internal/db` → `db` afterwards is a `git mv` plus an
import rewrite, deliberately deferred so no import path changes twice.

## Data layer rules

- **Handlers take `*dbgen.Queries`** (the concrete struct from `sqlcgen`), never
  an interface. `emit_interface: false` in `sqlc.yaml` keeps a wrong call a
  compile error.
- **Regenerate with `make sqlc`.** The output is committed, so a clone builds
  without sqlc installed; `make check-generated` regenerates and fails on drift
  when sqlc is on PATH (skips with a warning when it is not).
- **Migrations are additive files** under `internal/dbschema/migrations`,
  applied in filename order by `db.Migrate` on every boot and recorded in
  `schema_migrations`.
- **IDs are UUIDv7, generated in Go** (`db.NewID`), so the value exists before
  the insert and text ordering equals creation ordering.

## Auth: owned argon2id, with the library path written down

Sessions and password logins come from **`internal/auth`** (~300 owned lines):
argon2id hashes (`golang.org/x/crypto`, already a direct dependency), random
32-byte session tokens of which only the SHA-256 reaches the `sessions` table,
and the `gogogo_auth` cookie (`HttpOnly`/`SameSite=Lax`/`Secure`), so existing
browser sessions and the SSE/realtime auth path are unchanged. No secrets, no
config structs, no auth dependencies — sessions are random tokens, so there is
nothing to rotate.

**The path not taken: limen** (`github.com/thecodearcher/limen`). Evaluated
honestly in 2026-10: a composable auth library (sessions, cookies, CSRF, rate
limiting, plus magic-link/OAuth/2FA plugins) that fits Turso via its
database/sql adapter. Rejected for template core because it is pre-1.0 across
three version-skewed modules, owns a second schema (five tables with its own
columns and INTEGER ids, against the template's TEXT convention), and its
breadth (magic link, OTP, social) needs external infrastructure anyway — an
email sender, TOTP enrollment, OAuth client IDs — that no library eliminates.
Owning the commodity part (password + session + cookie) costs less long-term
than tracking a young library's surface across every generated project. If an
app outgrows password auth, limen remains the documented upgrade plugin for
exactly that moment.

## Auth plugins (owned): magic link, TOTP, OAuth

The three live in `internal/auth` (tables in `internal/dbschema/migrations`),
red-proofed against the real driver — no new auth dependencies:

- **Magic link** (`magiclink.go`): single-use tokens consumed by DELETE,
  15-minute expiry, silent nil for unknown addresses, no auto-provisioning
  (passwordless signup stays an explicit per-app choice, not a spam vector).
  Delivery is an injected `MagicSender` — the template has no opinion on
  SMTP vs API senders. Disabled without a sender (fail closed).
- **TOTP second factor** (`totp.go`): owned RFC 6238 (the usual library
  scored 0/10 maintained in 2026), verified against the RFC Appendix B
  vectors. `totp_secret` empty means off; `totp_verified` guards the
  enroll-then-confirm window so abandoned setups never lock anyone out.
  Password login withholds the session (`totp_required`) until
  `/api/auth/totp/login` closes it. The form login says so plainly —
  the shared form has no code field.
- **Social login** (`oauth.go`): generic authorization-code flow over
  `golang.org/x/oauth2` (already in the module graph), with Google and
  GitHub presets (GitHub reads `/user/emails` for the verified primary).
  Endpoints are data so tests point them at stubs. Links land in
  `oauth_accounts`; first login with a known address links instead of
  duplicating. Handshake states are short-lived and in-process (single-box
  caveat documented in code).

## Authorization: Casbin, embedded by default

Permissions are a Casbin model plus a policy file (`internal/rbac/casbin/`),
enforced by middleware before the handler runs: `admin` reaches the operator
surfaces, everyone else gets the product. Two roles cover the template; the
policy is data, so a generated app re-maps without touching handlers, and
`RBAC_POLICY_SOURCE=db` swaps the embedded file for the `casbin_rule` table when
a project needs runtime-editable rules.

The layer is deliberately thin: RBAC gates *routes*, and the UI only reflects
what the middleware enforces. Which role sees a link is cosmetic; which role
reaches the handler is the decision. Full guide: [RBAC](rbac.md).

## Why plugins live in-repo (not in plugin repos)

The split follows consumers, not size. Auth methods, TOTP, magic links and
the CRUD wire ship *with the template* — every generated project carries
them at the template's version, refactored atomically with it, under one
lint/test gate. Splitting them into versioned repos would buy independent
releases nobody needs (nothing consumes auth v1.2 without the template
around it) and charge per-change version bumps across repos.

`ai-credits` stays separate for the opposite reason: it is a *library* with
independent consumers (this template's demo, other apps), its own release
cadence, and a one-way dependency (template → lib, never back). Merging it
in would couple template releases to ledger releases with no consumer
benefit. Same rule, both directions: shared-versioned code lives together,
independently-consumed code lives apart.

**Plugin is a removal class, not a hosting location.** Genkit, GoAkt, DagNats,
NATS and the rest are third-party packages that the template *wires* — each is
registered in `internal/capabilities` with `SCOPE:plugin`, so the installer can
trim the unit and `go mod tidy` drops the dependency. A plugin is "removable
without touching core"; it says nothing about where the code is hosted. See
[Scope taxonomy](scope-taxonomy.md).

## Current state and remaining work

The migration is complete. The flat stack is the only stack: auth, the
todo slice (pages + data + realtime bridge, AI suggest, retry demo and the
durable onboarding workflow), the queue, whiteboard, notes, room, chat,
genui, credits, the config view, RBAC, the landing page, and the native GUI all
boot from `internal/server` on the same Turso + owned-auth data layer.

Gone with the framework: the `db/` package (app handle + seeds), the
`router/` package (route wiring), the `features/store` strategies
(PBStore and CRDTStore — one data layer remains, sqlc over `internal/db`),
`features/auth` (the legacy PB middleware), the PB handlers under every
feature, and the `internal/routeutil` + `internal/collabpb` helpers. One boot
path remains: `cfg.Host:cfg.Port` is the address, with `CHI_ADDR` as the
explicit override `cmd/desktop` sets so its webview proxy and the backend agree
([configuration](configuration.md)). `internal/db/` does not go away; it IS the
data layer.

The onboarding slice is the one place the two stacks genuinely differ
rather than duplicate a shape: the framework stack fans its stepper progress
out over JetStream, while the flat stack broadcasts to its own SSE hub. The
poll loop that owns a run is local to the instance that started it, and the
signal is ephemeral UI state, so no cross-instance guarantee is lost. The
step vocabulary is a handlers-local copy for the same reason the wire types are:
the engine package (`features/onboarding`) must not be imported from the todo
handlers, so the shared shape is duplicated rather than shared — the same
boundary the dependency gate enforces for the framework.

Conventions that outlive the transition: schema lives in
`internal/dbschema/migrations/*.sql`, applied in filename order and recorded in
`schema_migrations` — files are append-only history, never edited. Generated
`sqlcgen` is committed. Docs describe structure and contracts; CHANGELOG
keeps history.
