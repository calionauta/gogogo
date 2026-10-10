# Tech Plan: PocketBase Alternative Architecture (go-chi + limen + casbin + turso + sqlc + NATS/JetStream + SSE + Datastar)

## Frontmatter
product: gogogo
product_type: software
appetite: Core
approved: false
tech_stack:
  primary: "go 1.27 + go-chi v5 + templ v0.3.1020 + datastar v1.2.2"
  database: "turso (libSQL) via tursogo"
  auth: "limen"
  authorization: "casbin"
  orm/queries: "sqlc"
  realtime: "NATS JetStream + SSE Hub + Datastar"
  deployment: "docker"
  stack_source: "existing:go"

## Problem Statement
Replace PocketBase (app, auth, DB access, realtime record sync) with an explicit stack that fits gogogo's architecture (Datastar + Templ + SSE Hub + NATS/DagNats). Goal: preserve behavior (routes, cookie names, API shapes, Datastar contracts), improve testability (repo interfaces + sqlc), and keep the same realtime semantics where applicable while using NATS/JetStream for distribution.

## Constraints
- Must keep cookie name `gogogo_auth` unchanged
- Must preserve URL routes for all features (auth, todos, notes, whiteboard, genui, credits, store, etc.)
- Datastar fragments/SSE event shapes consumed by frontend must remain compatible where feasible
- Unified build (`go build ./cmd/web`) — no new build tags unless strictly necessary
- Respect project conventions: `log/slog`, no `log`, no `fmt.Sprintf` HTML, Datastar-first
- PB admin UI (:8090/_/) — decision needed: replace or remove (document in POC)
- Tests must continue to pass; Playwright smoke tests must still work
- Avoid secrets in code; follow existing env handling

## Codebase Reality (Preflight)
- Files indexed: 371, symbols: 2883
- PB references: ~211 Go files
- Entry: `db/pocketbase.go` (Init), `router/router.go` (wiring)
- Auth couples to PB tokens (`FindAuthRecordByToken`, dual cookie `pb_auth` for PB realtime)
- Realtime split: PB realtime + NATS/SSE Hub (whiteboard/notes collab already use NATS+SSE; todos use PB realtime path)
- Existing infra to reuse: `internal/queue/ssehub.go`, NATS/DagNats, Datastar, Templ

## Risks
- Large surface (211 files) — high blast radius
- Auth/realtime coupling to PB internals
- Data migration from PB SQLite to Turso/sqlc schema
- Dual-mode transition complexity
- Test harness assumptions (PB app instances passed around)

## Scopes

### SCOPE 01 — Infrastructure Foundation (TYPE: feature, Effort: Lean)
**Goal:** Establish the new data/auth/router/realtime foundation alongside existing PB (dual-mode ready).

**DoD:**
- [ ] `internal/dbsql/` (or `internal/db/alt`) created with sqlc config and base schema for core entities (users, todos, notes, whiteboards, credits)
- [ ] Turso client integration via tursogo (local dev path documented; prod via env)
- [ ] sqlc queries generated and buildable
- [ ] Go-Chi router wrapper in `internal/routerchi/` with same route surface contract
- [ ] limen auth skeleton in `internal/authlimen/` with token/cookie compatibility (keeps `gogogo_auth`)
- [ ] Casbin policies scaffold with roles (user, admin)
- [ ] NATS/JetStream event publisher scaffold wired to existing SSE Hub (subject naming `gogogo.{docType}.{docID}.{eventType}`)

**ACs:**
- AC1: `go build ./...` compiles with new packages present (no PB removal yet)
- AC2: sqlc generated code compiles
- AC3: Router chi wrapper exposes same method surface as needed by features
- AC4: Config/env documented for Turso (dev/prod)

**Tasks Table:**
| # | Task | Owner | Est (hrs) | Status |
|---|------|-------|-----------|--------|
| 1 | Create sqlc schema + config for core entities | eng | 2 | pending |
| 2 | Wire tursogo (dev/local + config) | eng | 1 | pending |
| 3 | Go-Chi base router + middleware | eng | 2 | pending |
| 4 | limen auth skeleton + cookie compatibility | eng | 2 | pending |
| 5 | Casbin RBAC scaffold | eng | 1 | pending |
| 6 | NATS/JS publisher + SSE Hub bridge | eng | 2 | pending |


## SEPARATE SECTION: POC (Proof of Concept)

### POC Goal
Validate the alternative stack with minimal surface area, zero behavior change for users, and ability to toggle implementations. Focus on **Auth + Credits** feature as the smallest viable slice to prove the full stack (auth, DB, routing, middleware).

### POC Scope (TYPE: spike, Effort: Lean)
**DoD:**
- [ ] Feature flag/env to select backend: `DB_BACKEND=pb|alt` (default `pb` initially)
- [ ] `internal/ports/` repo interfaces defined: `UserRepo`, `CreditsRepo` (minimal)
- [ ] Dual implementation: PB-backed repos (thin wrappers over current PB access) and sqlc-backed repos
- [ ] Auth migrated behind interface with limen (preserves `gogogo_auth` cookie, dual cookie strategy documented if needed for realtime)
- [ ] Credits feature uses repo interface (no direct PB calls in handler)
- [ ] Toggle works; PB path unchanged behavior; alt path compiles and basic flows work
- [ ] Admin UI decision documented: replace/remove + impact (keep as-is during POC or stub)

**ACs:**
- AC1: With `DB_BACKEND=pb`, credits + auth behavior unchanged (tests still green)
- AC2: With `DB_BACKEND=alt`, app builds and auth/credits basic flows functional against local DB
- AC3: No changes to templates/Datastar contracts
- AC4: Clear decision doc for admin UI in POC results

**POC Tasks:**
| # | Task | Owner | Est (hrs) | Status |
|---|------|-------|-----------|--------|
| 1 | Define ports/interfaces for auth+credits | eng | 1 | pending |
| 2 | PB repo adapters (preserve behavior) | eng | 1 | pending |
| 3 | sqlc schema/queries for users+credits | eng | 2 | pending |
| 4 | limen auth implementation behind interface | eng | 3 | pending |
| 5 | Wire credits handler to repo interface | eng | 1 | pending |
| 6 | Feature flag + DI wiring in router/bootstrap | eng | 1 | pending |
| 7 | Manual smoke + run existing related tests | eng | 2 | pending |
| 8 | Document admin UI decision + POC findings | eng | 0.5 | pending |

**POC Success Criteria:**
- ✅ Toggle works without behavior regression on PB path
- ✅ Alt path proves DB/auth stack end-to-end
- ✅ Minimal surface changed (credits+auth only)
- ✅ Clear path to expand to other features

**POC Exit:** If all ACs pass, proceed to full migration (Scope 02+). If blockers found, document and adjust plan.

## Remaining Scopes (Post-POC)

### SCOPE 02 — Auth Full Migration (TYPE: feature, Effort: Core)
**Goal:** Complete auth migration behind ports; preserve cookie + token semantics.

**DoD:** Auth flows (login/logout, session restore) work via limen on alt path; PB path unchanged. Tests updated/green.

**ACs:** AC1: Login/logout + session restore works. AC2: No account enumeration semantics preserved. AC3: Cookie `gogogo_auth` unchanged.

**Dependencies:** SCOPE 01, POC

### SCOPE 03 — Core Data Repos (TYPE: feature, Effort: Core)
**Goal:** Add repos for todos, notes, whiteboards, users (full); migrate features to ports incrementally with dual impl.

**DoD:** Repo interfaces + sqlc impl + PB adapters for each; features wired behind interfaces.

**ACs:** AC1: Build passes. AC2: Existing unit/integration tests green on PB path.

**Dependencies:** SCOPE 01

### SCOPE 04 — Realtime Migration (TYPE: feature, Effort: Core)
**Goal:** Replace PB realtime usage with NATS/JetStream + SSE Hub. Publish events on mutations; subscribe via Datastar.

**DoD:** Event subjects standardized; todos/whiteboard/notes emit/listen via NATS; PB realtime path removed where feasible or gated.

**ACs:** AC1: Cross-tab sync preserved. AC2: SSE streams stable. AC3: Playwright smoke/whiteboard tests still pass.

**Dependencies:** SCOPE 01, SCOPE 03

### SCOPE 05 — Feature Cutover & Tests (TYPE: feature, Effort: Core)
**Goal:** Migrate all features to use alt repos by default (or keep gated), update tests to use interfaces, add integration tests with Turso.

**DoD:** PB app instance usage minimized; tests pass on both paths where applicable.

**ACs:** AC1: Full test suite green. AC2: No direct PB API calls in feature handlers (enforced via review).

**Dependencies:** SCOPE 02, SCOPE 03, SCOPE 04

### SCOPE 06 — Cutover & Cleanup (TYPE: feature, Effort: Lean)
**Goal:** Remove PB dependency (`go.mod`), delete PB-specific code (`db/pocketbase.go`, seeds/hooks tied to PB), update docs, flip default to alt (or document rollout).

**DoD:** PB removed; build/tests green; docs updated; secrets/env documented.

**ACs:** AC1: `go mod tidy` clean; no PB imports remain. AC2: CI checks pass. AC3: Migration guide for data.

**Dependencies:** SCOPE 05

## Sequencing
1. POC (Auth+Credits) — validates stack, dual-mode, toggle
2. SCOPE 01 (infra) — if POC revealed gaps, complete foundation
3. SCOPE 02-04 — incremental migration behind interfaces
4. SCOPE 05-06 — cutover

## Testing Strategy (AI-Aware, software)
- Unit: sqlc repos + limen + Casbin logic; use fakes/mocks via interfaces
- Integration: Turso local (sqld) or in-memory libSQL; test repo contracts
- E2E: Playwright (smoke, whiteboard, notes caret) — must remain green
- Regression: keep PB path green during dual-mode; add contract tests for interfaces
- Security: auth token validation, Casbin policy checks, no secrets leakage

## Technical Details

### Repo Interfaces (POC)
```go
// internal/ports/auth.go
type AuthService interface {
  IssueCookie(w http.ResponseWriter, userID string) error
  ClearCookie(w http.ResponseWriter)
  LoadFromRequest(r *http.Request) (userID string, ok bool)
}

// internal/ports/users.go
type UserRepo interface {
  GetByID(ctx context.Context, id string) (*User, error)
  GetByEmail(ctx context.Context, email string) (*User, error)
  Create(ctx context.Context, u *User) error
  Update(ctx context.Context, u *User) error
}

// internal/ports/credits.go
type CreditsRepo interface {
  GetBalance(ctx context.Context, userID string) (int64, error)
  Adjust(ctx context.Context, userID string, delta int64, reason string) error
}
```

### Env Vars (new)
- `DB_BACKEND=pb|alt` (default `pb` for safety)
- `TURSO_URL` / `TURSO_TOKEN` (prod) or `TURSO_DB_PATH` (local dev)
- `NATS_URL`, `NATS_JETSTREAM` flags (reuse existing)
- `AUTH_SECRET` (for limen tokens/cookies)

### sqlc Structure
- `db/sqlc/schema.sql` - DDL
- `db/sqlc/queries/*.sql` - CRUD queries
- `db/sqlc/sqlc.yaml` - config
- Generated: `internal/dbsql/models.go`, `internal/dbsql/queries.go` (or `db/sqlc/gen/...`)

### Directory Layout (new)
- `internal/ports/` - repo/service interfaces
- `internal/repos/pb/` - PB-backed adapters (preserve behavior)
- `internal/repos/alt/` - sqlc-backed implementations
- `internal/authlimen/` - limen auth service impl
- `internal/dbsql/` - sqlc generated + client setup
- `internal/routerchi/` - chi router wrapper + middleware
- `internal/casbinpol/` - policies + enforcer
- `internal/events/` - NATS/JS publisher helpers

### Go-Chi Integration
Wrap chi in a thin router that satisfies existing call sites if possible. Keep route registration order/paths identical. Middleware stack: request id, slog, recovery, auth (behind interface), CSRF if applicable, etc.

### Realtime Eventing
Subject: `gogogo.{kind}.{id}.{action}` (e.g., `gogogo.todo.abc123.updated`). Publish on mutations in alt path. Existing SSE Hub subscribes to these events and broadcasts to Datastar clients. Maintain backward compatibility with PB realtime during dual-mode.

### Migration Notes
- Keep dual cookie strategy documented: app uses `gogogo_auth`; PB paths may still need `pb_auth` until PB realtime fully removed.
- Data export: write script to migrate PB SQLite collections to sqlc schema (idempotent, preserves IDs).
- Tests: add build tags only if unavoidable; prefer interface fakes to avoid dual-path test complexity.
