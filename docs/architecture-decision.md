# Architecture decision: flat stack over framework + abstraction

Status: accepted (2026-10-11)

## Decision

gogogo targets an explicit, flat stack: **chi** for routing, **sqlc-generated
functions called directly from handlers** for data access, **Turso Database**
(the Rust SQLite-compatible engine, driven by `turso.tech/database/tursogo`),
**owned argon2id auth** (`internal/auth`: password hashes, opaque sessions,
`gogogo_auth` cookie, zero auth dependencies), **casbin** for permissions,
and **Datastar + Templ** for the UI. PocketBase, the limen auth library and
the repo-interface abstraction layer (`internal/ports`, `internal/repos`,
`internal/backend`) are removed.

## Why (evidence)

**Shallow abstractions hurt LLM work.** Matt Pocock, *9 Ways AI Coding Has
Rewired My Brain* (2026): "Bad codebases have lots of shallow modules with big
interfaces. Good codebases have a few big modules with simple interfaces." The
removed layer was three tiny interfaces with two one-line adapters each — a
shallow pass-through that added files and deferred errors from compile time to
runtime.

**Direct calls catch errors earlier.** Calling `dbgen.CreateTodo` directly turns
a wrong query name, a missing argument, or a changed column into a compile
error instead of an interface mismatch found at runtime. That is the tight
feedback loop AI coding depends on.

**Turso, not libSQL.** Turso Database is the ground-up Rust rewrite of SQLite;
`tursogo` is its Go driver. libSQL is the older fork and is not used.

**Counterpoint, kept honest.** An abstraction is justified when there are
genuinely interchangeable implementations — e.g. swapping LLM providers. The
`internal/llm` client stays a (deep) module for exactly that reason.

## Target directory structure

| Concern | Location |
|---|---|
| HTTP handlers (chi) | `features/<name>/handler.go` |
| DB connection, pragmas, boot | `internal/db/db.go` |
| Schema (the migrations ARE the schema) | `internal/db/migrations/*.sql` |
| sqlc query sources | `internal/db/queries/<domain>.sql` |
| sqlc config | `internal/db/sqlc.yaml` |
| sqlc-generated Go (`package dbgen`) | `internal/db/sqlcgen/` |
| Auth (owned, argon2id + sessions) | `internal/auth/` |
| Casbin model + policy (embedded) | `internal/rbac/casbin/` |
| RBAC middleware | `internal/rbac/` |
| Business logic | `features/<name>/` |

Everything data-shaped lives under one tree, `internal/db/`, so there is exactly
one connection string, one schema, and one generated package to reason about.
PocketBase's old `db/` package is deleted with it, which frees the top-level
`db/` name; promoting `internal/db` → `db` afterwards is a `git mv` plus an
import rewrite, deliberately deferred so no import path changes twice.

## Data layer rules

- **Handlers take `*dbgen.Queries`** (the concrete struct from `sqlcgen`), never
  an interface. `emit_interface: false` in `sqlc.yaml` keeps a wrong call a
  compile error.
- **Regenerate with `make sqlc`.** The output is committed, so a clone builds
  without sqlc installed; `make check-generated` regenerates and fails on drift
  when sqlc is on PATH (skips with a warning when it is not).
- **Migrations are additive files**, applied in filename order by
  `db.Migrate` on every boot and recorded in `schema_migrations`.
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

The three live in `internal/auth` (tables in `internal/db/migrations`), red-proofed against the
real driver — no new auth dependencies:

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

## Turso Database: the cost, stated plainly

`tursogo` is a `database/sql` driver (`sql.Open("turso", path)`) loaded through
purego, so there is **no CGO** and the cgo-free cross-compilation story holds.

Two things are worth knowing before this becomes the only engine:

- The module `github.com/tursodatabase/turso-go-platform-libs` carries a prebuilt
  Rust shared library per platform (~15 MB each, ~124 MB of module). The build
  tag selects one; at first use it is written to a cache directory
  (`TURSO_GO_CACHE_DIR`, else `os.UserCacheDir()`), verified against an embedded
  SHA-256, and `dlopen`ed. A container whose cache directory is unwritable needs
  `TURSO_GO_CACHE_DIR` pointed at a writable path.
- Turso Database is pre-1.0. Keep backups (`scripts/backup.sh`) — the driver's
  own README says the same thing.

## Current state and remaining work

The flat stack serves auth, the todo slice (pages + data + realtime bridge,
AI suggest, retry demo and the durable onboarding workflow), the queue, and
the remaining feature surfaces (whiteboard, notes, room, chat, genui,
credits, config view, landing). What still runs on the previous stack: the
desktop GUI boot. Everything is ported with the same recipe (contract tests
first, same paths and wire, no translation layers), then `db/` goes away
with the framework dependency, the framework-only tests with it, and the
transitional serve seam (`CHI_ADDR`) with them.

The onboarding slice is the one place the two stacks genuinely differ
rather than duplicate a shape: the framework stack fans its stepper progress
out over JetStream, while the flat stack broadcasts to its own SSE hub. The
poll loop that owns a run is local to the instance that started it, and the
signal is ephemeral UI state, so no cross-instance guarantee is lost. The
step vocabulary is a chi-local copy for the same reason the wire types are:
`features/todo/chi` cannot import `features/todo/handlers` without dragging
the framework into the flat stack, which the dependency gate forbids.

Conventions that outlive the transition: schema lives in
`internal/db/migrations/*.sql`, applied in filename order and recorded in
`schema_migrations` — files are append-only history, never edited. Generated
`sqlcgen` is committed. Docs describe structure and contracts; CHANGELOG
keeps history.
