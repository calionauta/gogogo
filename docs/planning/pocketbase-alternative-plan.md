# Plan: PocketBase Alternative Architecture for gogogo

**Author:** Analysis via Codebuff  
**Date:** 2025-10-10  
**Location:** /Users/cali/Development/gogogo-template/docs/planning/pocketbase-alternative-plan.md

## Executive Summary

This document outlines a comprehensive plan to replace PocketBase with an alternative stack while maintaining full compatibility with gogogo's existing architecture. The proposed stack is:
- **Routes:** Go-Chi (lightweight, idiomatic, middleware-heavy routing)
- **Auth:** [limen](https://github.com/thecodearcher/limen) (Go auth library)
- **Permissions:** [Casbin](https://casbin.org/) (RBAC/ABAC authorization)
- **Database:** [Turso](https://turso.tech/) (libSQL fork) with [tursogo](https://github.com/tursodatabase/tursogo) (Turso Go client)
- **Schema/Queries:** [sqlc](https://sqlc.dev/) (type-safe SQL to Go code generation)
- **CLI/MCP:** Turso CLI in MCP mode for tooling
- **Realtime:** NATS/JetStream for event distribution + SSE Hub for subscribers + Datastar for UI updates

## Current State Analysis

### PocketBase Integration Points
Based on codebase analysis:
- **Core Init:** `db/pocketbase.go` - PB app initialization with custom SQLite driver config
- **Schema/Models:** PB collections defined via hooks/seeds (`db/seed*.go`, `db/idempotency_*.go`)
- **Auth:** Cookie-based auth via `gogogo_auth` cookie (not PB's default), auth middleware in `features/auth/`
- **DB Access:** Extensive use of `app.FindRecordsByFilter()`, `app.FindRecordById()`, `app.Save()`, `record.GetString()`, etc. (~211 Go files reference PB)
- **Realtime:** PB realtime subscriptions used in features like todo, whiteboard (collab uses NATS + SSE Hub; PB realtime also used)
- **Admin UI:** Separate origin on :8090/_/ 
- **Collections:** todos, notes, whiteboards, users, pt_*, credits, genui runs, etc.

## Proposed Architecture

### 1. Data Layer (Turso + sqlc)

#### Schema Design
- Define SQL schema in `db/schema.sql` (or migrations in `db/migrations/`)
- Use sqlc to generate type-safe Go code from queries
- Each entity (users, todos, notes, whiteboards, etc.) gets models + CRUD queries
- Maintain backward compatible field names where possible to minimize template changes

#### Turso Integration
- Use `tursogo` client for libSQL connections (Turso is a libSQL-compatible fork)
- Support both local dev (sqld/libSQL) and production (Turso remote)
- Connection pooling and transaction management
- WAL mode, busy timeout pragmas similar to current PB config

### 2. Routing (Go-Chi)
- Replace PB's internal router with Go-Chi
- Maintain same URL structure for backward compatibility
- Middleware: auth, logging (slog), CORS, CSRF, rate limiting as needed
- Route groups by feature (todos, notes, whiteboard, genui, etc.)

### 3. Authentication (limen)
- Replace PB auth with limen
- JWT-based tokens or session tokens
- Password hashing (bcrypt/argon2)
- Email/password auth, token validation
- Maintain same cookie name (`gogogo_auth`) for compatibility
- Generate tokens that can be verified without PB

### 4. Authorization (Casbin)
- Define policies in Casbin model/policy files
- Roles: user, admin (superuser equivalent)
- Resource-level permissions (own records vs others)
- Enforce in middleware/handlers

### 5. Realtime (NATS/JetStream + SSE Hub)
- Use existing NATS infrastructure where available (project already has NATS/DagNats)
- JetStream for persistent event streams
- Subject naming: `gogogo.{docType}.{docID}.{eventType}`
- SSE Hub (`internal/queue/ssehub.go`) manages client connections per topic
- Datastar on frontend subscribes to SSE streams and patches UI
- Keep separate hub per feature if needed (e.g., whiteboard separate from todos)

### 6. Feature Compatibility Layer
- Abstract DB operations behind interfaces per feature to minimize changes
- Create adapters to map between current PB-style access and new sqlc queries
- Preserve handler signatures where possible
- Keep same response formats for APIs consumed by Datastar

## Migration Strategy (Phased Approach)

### Phase 0: Preflight & Analysis (Week 0-1)
- [ ] Inventory all PB API usage across codebase (done partially - 211 files)
- [ ] Document all collection schemas (todos, notes, whiteboards, users, credits, etc.)
- [ ] Map auth flows end-to-end
- [ ] Map realtime flows (PB realtime vs NATS/SSE)
- [ ] Identify PB-specific features to replicate or remove (admin UI, file storage, etc.)
- [ ] Set up Turso dev instance, test tursogo connectivity
- [ ] Test Turso CLI MCP integration

### Phase 1: Infrastructure Foundation (Week 1-2)
- [ ] Create `internal/db` package with sqlc + Turso setup
- [ ] Define SQL schema for all collections
- [ ] Generate sqlc queries
- [ ] Implement model mapping (PB record style → Go structs)
- [ ] Set up Go-Chi router with base middleware
- [ ] Create `internal/auth` with limen integration
- [ ] Set up Casbin policies

### Phase 2: Auth System Migration (Week 2-3)
- [ ] Replace auth middleware (`features/auth/`)
- [ ] Migrate login/logout flows
- [ ] Maintain cookie compatibility (`gogogo_auth`)
- [ ] Handle both app auth and potential PB compatibility during transition
- [ ] Update tests

### Phase 3: Data Access Abstraction (Week 3-4)
- [ ] Create repository interfaces per feature (TodosRepo, NotesRepo, etc.)
- [ ] Implement sqlc-based repositories
- [ ] Add feature flags to switch between PB and new impl
- [ ] Migrate core features (todos, auth, credits) first
- [ ] Keep backward compatibility during dual-mode

### Phase 4: Realtime Migration (Week 4-5)
- [ ] Enhance SSE Hub if needed
- [ ] Wire NATS JetStream for event publishing
- [ ] Update features to publish events on mutations
- [ ] Update Datastar subscriptions
- [ ] Migrate whiteboard/notes collab to use new realtime path
- [ ] Deprecate PB realtime where used

### Phase 5: Feature-by-Feature Migration (Week 5-6)
- [ ] Migrate features in order: auth → todos → notes → whiteboard → genui → credits → store → etc.
- [ ] Update each feature's handlers to use new repositories
- [ ] Update templates if needed (minimal changes)
- [ ] Run feature tests after each migration

### Phase 6: Cutover & Cleanup (Week 6-7)
- [ ] Remove PB dependency from go.mod
- [ ] Remove PB init code, seeds, hooks
- [ ] Remove PB-specific admin UI routes if not replaced
- [ ] Update docs
- [ ] Full test suite validation
- [ ] Performance testing

## Technical Considerations

### Dependencies to Add
```go
// New deps
github.com/go-chi/chi/v5 v5.x
github.com/thecodearcher/limen vX.Y.Z  // check latest
github.com/casbin/casbin/v2 v2.x
github.com/tursodatabase/tursogo vX.Y.Z
github.com/sqlc-dev/sqlc v1.x (tool, not runtime)
// NATS already present (github.com/nats-io/nats.go)
```

### Backward Compatibility Concerns
- **Cookie names:** Keep `gogogo_auth` exactly same
- **URL routes:** Must remain identical for all features
- **API contracts:** Datastar expects specific response formats
- **SSE event shapes:** Keep compatible where consumed by frontend
- **Templates:** Minimize changes to `.templ` files

### Risks & Mitigations
| Risk | Impact | Mitigation |
|---|---|---|
| Large migration surface (211 files) | High | Use feature flags, phased migration, repo interfaces |
| Auth incompatibility with PB realtime | High | Keep dual cookie approach; or replace PB realtime fully with NATS |
| Data migration from PB SQLite | Medium | Write migration script to export PB data and import to Turso/sqlc schema |
| Test coverage gaps | Medium | Run tests after each phase; add integration tests |
| Performance differences | Low | Benchmark key operations, tune pragmas on Turso |
| limen/Casbin learning curve | Medium | Start with minimal auth, simple policies |

### Testing Strategy
- **Unit tests:** Update to use new repositories with in-memory/test DB
- **Integration tests:** Use testcontainers or local Turso instance
- **E2E tests:** Playwright tests should still pass (whiteboard, notes, smoke)
- **Regression tests:** All existing tests that don't depend on PB internals

## Implementation Recommendations

1. **Start small:** Create a proof-of-concept with just auth + one simple feature (e.g., credits) to validate the stack
2. **Abstract early:** Create interfaces before implementation - this makes swapping easier
3. **Keep PB during transition:** Use build tags or env vars to switch implementations
4. **Preserve behavior:** Don't change business logic, just plumbing
5. **Leverage existing infra:** NATS/JetStream already there; SSE Hub exists; Datastar patterns established
6. **Use sqlc generated code:** Type safety + compile-time checks

## Success Criteria
- All existing tests pass
- No breaking changes to user-facing behavior
- Performance comparable to PB
- Cleaner separation of concerns
- Easier to test (mockable repos)
- Full type safety with sqlc

## Preflight Findings (via cymbal/ripwire)

- **Codebase size:** 371 files, 2883 symbols indexed by cymbal
- **PB references:** 211 Go files import/use PocketBase API
- **Key entry points:** `db.Init()` and `router.Init()` wire PB app
- **Core patterns:** App instance passed around, records accessed via PB API, auth via PB tokens
- **Existing infra to reuse:** NATS/DagNats, SSE Hub (`internal/queue/ssehub.go`), Datastar integration already mature
- **Admin UI:** `/` dashboard on :8090/_/ - needs decision: replace or remove

## Recommended Next Action

Start with **Phase 0.1 (Proof of Concept)**: Implement minimal stack (Go-Chi + limen + sqlc + Turso) for just the `credits` feature or `auth` to validate the approach before full migration. This will surface integration issues early with minimal risk.
