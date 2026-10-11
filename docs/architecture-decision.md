# Architecture decision: flat stack over framework + abstraction

Status: accepted (2026-10-11)

## Decision

gogogo targets an explicit, flat stack: **chi** for routing, **sqlc-generated
functions called directly from handlers** for data access, **Turso Database**
(the Rust SQLite-compatible engine, driven by `turso.tech/database/tursogo`),
**limen** for auth, **casbin** for permissions, and **Datastar + Templ** for the
UI. PocketBase and the repo-interface abstraction layer (`internal/ports`,
`internal/repos`, `internal/backend`) are removed.

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
| Auth (limen) | `internal/auth/` |
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

## Auth: limen, with the alternative written down

Sessions and password logins come from **limen**
(`github.com/thecodearcher/limen`), a composable auth library: sessions, cookie
handling, CSRF and rate limiting in the core, each method composed on top. It
replaces PocketBase's auth collection, and the app keeps the `gogogo_auth`
cookie name so existing browser sessions and the SSE/realtime auth path are
unchanged.

**The alternative, if limen ever stops fitting: argon2id + a sessions table.**
Both pieces already exist in this repo's dependency set — `golang.org/x/crypto`
(already a direct dependency) provides argon2id, and
`internal/db/migrations/0001_init.sql` already defines a `sessions` table
storing only the hash of the cookie value. That path is roughly 150 lines of
straight-line code (hash on register, verify on login, random 32-byte token
stored as SHA-256, `Set-Cookie` with `HttpOnly`/`SameSite=Lax`/`Secure`), no new
dependency, and no library between the request and the database. Choose it if
limen's surface becomes the thing you are debugging.

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

## Migration plan (staged; every step ends green)

1. **Done** — abstraction removed, Turso naming fixed, `POC` naming gone.
2. **Done** — `internal/db`: Turso connection, `_busy_timeout` DSN, pragmas,
   migration runner, core schema (users, sessions, todos), sqlc config +
   queries + committed `sqlcgen`, and tests that run every one of them against
   the real driver.
3. **Done** — Auth on limen as chi middleware (`internal/auth`, red-proofed
   against the real Turso driver): credential/password plugin on its own
   `limen_*` tables (migration 0002; limen's `users`/`sessions` names would
   collide with the app tables from 0001, so every limen table carries the
   prefix via Schema options), session cookie kept as `gogogo_auth`
   (`WithHTTPSessionCookieName`), chi `RequireSession` middleware with
   `SessionFrom` accessor. Not yet wired into `server.Run` — the PocketBase
   boot still serves the app; the wiring lands with the todo slice (step 4).
4. **Done (data endpoints)** — todo slice on chi + `dbgen` (`features/todo/chi`,
   red-proofed contract tests + live preflight): list/fragment/create/toggle/
   delete/clear/confirm/stream with the same paths, statuses, redirects and
   Datastar SSE wire as the PocketBase handlers. Identity unified on limen
   (migration 0003 drops the unused 0001 users/sessions tables; todos.owner
   is now INTEGER REFERENCES limen_users). Out of scope here on purpose:
   full page (/todo index), AI suggest, retry demo, onboarding (step 6), and
   NATS cross-instance publish (step 5). Served alongside PocketBase only
   when CHI_ADDR + LIMEN_SECRET are set (`internal/server.StartChi`).
5. Port realtime (SSE Hub already exists; drop PocketBase realtime).
6. Port the remaining features (whiteboard, notes, room, chat, genui, credits,
   config view, landing, desktop GUI).
7. Delete `db/`, drop the PocketBase dependency from `go.mod`, delete
   PocketBase-only tests, add contract tests for the flat paths.

Full PocketBase removal is a staged rewrite, not one change: it touches every
handler and roughly 130 test files. Doing it in a single pass would leave the
tree broken, so each stage ships independently.
