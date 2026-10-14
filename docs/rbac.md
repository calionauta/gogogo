# RBAC (Casbin): the complete guide

Role-based access control, opt-in via `RBAC_ENABLED=true`. Casbin
decides; `internal/auth` authenticates. If you are an LLM adding a
feature, read "Adding a feature" and follow it mechanically — the
tests fail naming the exact gap when you skip a step.

## How a request flows

1. The middleware (`internal/rbac/middleware.go`, wired once in
   `BootChi`) resolves the session to a **subject**: `admin`, `user`,
   `viewer`, or `anonymous`. Unknown `users.role` values fall back to
   `user` — never up, never lockout.
2. It calls `Enforce(subject, path, method)` against the embedded
   policy. Deny (or enforcer error) answers **403 with no body** and a
   warn log carrying subject/object/action.
3. Disabled (`RBAC_ENABLED=false`) the middleware passes everything
   through with identical wiring.

## File map

- **`internal/rbac/casbin/model.conf`** — the model. Request, policy
  and matchers; change it only to change the authorization *model*
  (you won't — RBAC + RESTful covers this template).
- **`internal/rbac/casbin/policy.csv`** — the rules. The only file you
  edit for routine changes. Compiled into the binary via `//go:embed`,
  so no config file is read at runtime.
- **`internal/rbac/rbac.go`** — builds the enforcer once; `EnforceWith`
  reuses it on the hot path. There is deliberately no per-call helper
  that rebuilds it.
- **`internal/rbac/dynamic.go`** — the ABAC `Object` and the matcher
  functions `pathMatch` / `attrOK` the model names.
- **`internal/rbac/adapter.go`** — the DB-backed `persist.Adapter`
  (`RBAC_POLICY_SOURCE=db`) and the one-time seed from `policy.csv`.
- **`internal/rbac/roles.go`** — the role vocabulary: constants,
  `IsViewer` display hint, `subjectOf` resolution, and why roles stay
  flat. One home for the concept "role".
- **`internal/rbac/middleware.go`** — the HTTP wrapper, including
  permissive audit mode. Thin by design.
- **`internal/server/boot_helpers.go`** (`applyRBAC`, `promoteRole`) —
  builds the enforcer, promotes `VIEWER_EMAILS` then `ADMIN_EMAILS`
  rows (ties end `admin`), wraps the mux.

## The model, in one paragraph

Every decision is `Enforce(subject, obj, act)` where `obj` is a path
string or an ABAC `Object` (see "Dynamic permissions"). A row `p, user,
/todo/*, .*` allows when the subject has the role (`g`), the path
matches (`pathMatch`, which is `keyMatch2`: `*` spans `/`), the method
matches (`regexMatch`: `(GET|POST)` or `.*`), and `attrOK` passes. Effect:
any allow wins. No `g,` rows ship — roles are flat (see below).

## Policy row anatomy

- **Blocks are ordered by role**: `admin` (`/*` — everything), then
  `anonymous` (public minimum), then `user` (whole subtrees), then
  `viewer` (enumerated paths only). Keep a new row inside its role's
  block, paths sorted.
- **Subject must be exact**: `admin`, `user`, `viewer`, `anonymous. A
  typo is a dead row — fail-closed, no error, only the shape test
  sees it.
- **Users get subtrees** (`/prefix`, `/prefix/*`); **viewers get
  enumerated paths**. Viewer wildcards outside `/todo/*`, `/static/*`,
  `/api/auth/*`, `/api/todos/*/toggle` fail the shape test.
- **Fail-closed by construction**: a route with no row denies everyone
  but admin. Adding a route without adding its rows is a 403 bug, and
  the coverage test names it.

## Adding a feature (checklist)

1. Mount routes in `internal/server/chi.go` `buildChiMux` (one arg per line).
2. Add `p, user, /prefix, .*` (+ `/prefix/*`) rows in the user block.
3. Add enumerated viewer rows only if the feature has a viewer-safe
   surface (read/create/toggle class — never deletes, never paid).
4. Extend `TestRBACPolicyCoversMuxRoutes` with the new mounts.
5. Run `go test -count=1 ./internal/rbac/` (~2s; the
   `rbac-policy` pre-commit hook runs the shape subset on commits
   touching `internal/rbac/`).
6. Roll out with `RBAC_PERMISSIVE=true` for one deploy, read the
   `rbac: permissive allow (would deny)` lines, add the missing rows,
   then turn it off. Production posture is permissive=false.

## Roles: vocabulary, promotion, flatness

- **Vocabulary**: four subjects, owned by `roles.go`. Ties lose to
  admin (viewer promotion runs first).
- **Promotion** (`ADMIN_EMAILS` / `VIEWER_EMAILS`, comma-separated,
  matched lowercase): existing rows only, never provisions, unknown
  addresses skip silently. Set them, reboot, done.
- **Flatness**: Casbin inheritance only ADDS permissions, and viewer
  is a SUBSET of user — `g, viewer, user` would escalate every viewer.
  The subset is enumerated paths, not hierarchy. Do not add a `g` line.

## Display hint vs enforcement

`rbac.IsViewer(role)` is a DISPLAY hint only (disable buttons, show the
reason). Enforcement stays in `policy.csv`, so a wrong hint degrades to
a 403, never to an unauthorized write. Never gate a mutation on
`IsViewer` in a handler — the middleware already denied it.

## Env reference

- **`RBAC_ENABLED`** (default `false`) — enforce or pass through.
- **`RBAC_PERMISSIVE`** (default `false`) — audit-before-enforce;
  would-be denies logged with `permissive=1` and allowed through.
- **`RBAC_POLICY_SOURCE`** (default `embedded`) — `embedded` reads the
  compiled-in `policy.csv`; `db` loads the `casbin_rule` table instead.
  Any other value falls back to `embedded`.
- **`ADMIN_EMAILS` / `VIEWER_EMAILS`** (unset) — boot promotion lists.

## Test map (what pins what)

- **Shape** (`TestRBACPolicyShape`): subjects, paths, acts, viewer
  allowlist — the typo catcher. Pre-commit subset.
- **Flat + dedupe**: no `g` rows, no duplicate lines.
- **Coverage** (`TestRBACPolicyCoversMuxRoutes`): every mux mount
  decided for user-or-admin; `/config`, `/console`, `/dagnats` denied
  for user.
- **Viewer matrix + middleware**: allow/deny decisions plus a real
  viewer through the real middleware (create/toggle pass, delete 403s).
- **Display** (`TestTodoViewerDeleteAffordanceDisabled`,
  `TestTodoSuggestTabVisibility`): the UI narrates the role.
- **Boot wiring** (`TestApplyRBACWiresPermissive`,
  `TestPromoteViewersPinsViewerContract`): flags and promotion reach
  the middleware.

## Adapting this guard for your product

The tests pin *this* template's choices. Downloaders with new roles or
surfaces hit them by design — each failure tells you what to decide:

- **New role** (e.g. `editor`): add it to `roles.go`, to
  `knownSubjects`, to `policy.csv`, and to the promotion path.
- **Role hierarchy** (e.g. `owner` inherits `editor`): the flat test
  fails on purpose — delete it deliberately and document why your
  hierarchy cannot escalate anyone, or keep roles flat.
- **Viewer-reachable wildcard** (e.g. read-only `/reports/*`): prefer
  enumerated paths; if the subtree is genuinely safe, extend
  `viewerWildcards` with a one-line justification.
- **New feature paths**: extend the coverage test (fail-closed
  otherwise) and mirror the user block.

## Dynamic permissions

Three dynamic inputs sit on top of the static policy. All are no-ops for
a bare path string, so the default decisions never change.

**1. ABAC attributes** (`internal/rbac/dynamic.go`). The request `obj`
may be an `Object` instead of a path string:

```go
rbac.Object{Path: "/api/todos/abc", SubjectID: userID, Owner: rec.Owner}
```

The matcher's `attrOK(r.sub, r.obj)` passes a bare string and an
ownerless Object; an Object with an owner allows admin or the owner
(`SubjectID == Owner`). The middleware already passes an Object carrying
`Path`, `SubjectID` and the client `IP`; a surface that needs ownership
fills `Owner` from its own record loader. Add a tenant or IP rule by
extending `attrsOK` — no model change needed.

**2. Code-defined conditions.** The matcher functions are plain Go, so
any conditional (a computed flag, a variable, a DB read) can gate a
decision: register another function with `e.AddFunction("myCond", fn)`
in `dynamic.go` and name it in `model.conf`. This is the seam for
"permissions from code".

**3. Policy in the database** (`internal/rbac/adapter.go`). With
`RBAC_POLICY_SOURCE=db` the enforcer loads its rules from the
`casbin_rule` table instead of `policy.csv`. The first boot seeds the
table from the embedded policy, so switching source changes no decision
until a row is edited; an insert/delete then takes effect on the next
build. `DBAdapter` is a full `persist.Adapter`, so `e.AddPolicy` /
`e.RemovePolicy` write through to the table too. The embedded policy
stays the default and is still what the shape/coverage tests pin.

## What RBAC does not do

- **No per-record sharing / ReBAC** — single-owner ABAC ships (see
  above); sharing one record between users is still a per-feature job.
- **No domains** (multi-tenancy): one role set per user until a second
  tenant exists. `Object.Tenant` is a reserved hook.
- **No deny-override or priority lines**.
