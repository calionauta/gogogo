# RBAC (Casbin)

Role-based access control, opt-in via `RBAC_ENABLED=true`. Enforcement is
Casbin (model + policy compiled into the binary); authentication stays in
`internal/auth`. This page is the authoring checklist — model theory lives
in the [Casbin docs](https://casbin.apache.org/docs/how-it-works).

## Vocabulary (one home)

`internal/rbac/roles.go` owns the four subjects:

| Subject | Source | Policy |
|---|---|---|
| `admin` | `ADMIN_EMAILS` | `/*` — everything |
| `user` | any other session | feature subtrees, `.*` per subtree |
| `viewer` | `VIEWER_EMAILS` | enumerated todo paths only (read + create + toggle) |
| `anonymous` | no session | static, health, `/`, login, signup |

Ties lose to admin (viewer promotion runs first). Unknown `users.role`
values fall back to `user` — never up.

## Why the roles are flat

Casbin inheritance (`g, a, b`) only ADDS permissions. Viewer is a
SUBSET of user, so `g, viewer, user` would escalate every viewer to a
full user. The subset is expressed by enumerating viewer paths, not by
hierarchy. Do not add a `g` line between them.

## Adding a feature (checklist)

1. Mount routes in `internal/server/chi.go` `buildChiMux` (one arg per line).
2. Add `p, user, /prefix, .*` (+ `/prefix/*`) rows to
   `internal/rbac/casbin/policy.csv`, in the user block, paths sorted.
3. If the feature has a restricted surface, add enumerated viewer rows
   (never `.*` on a write subtree).
4. Extend `TestRBACPolicyCoversMuxRoutes` (`internal/rbac/rbac_test.go`)
   with the new paths — the test mirrors the mux and fails naming the
   gap (fail-closed: unlisted routes deny everyone but admin).
5. Roll out with `RBAC_PERMISSIVE=true` for one deploy, read the
   `rbac: permissive allow (would deny)` lines, add the missing rows,
   then turn it off. Production posture is permissive=false.

## Display hint vs enforcement

`rbac.IsViewer(role)` is a DISPLAY hint only (disable buttons, show the
reason). Enforcement stays in `policy.csv`, so a wrong hint degrades to
a 403, never to an unauthorized write. Never gate a mutation on
`IsViewer` in a handler — the middleware already denied it.

## What RBAC does not do

No ownership ("only their own"), no time/IP context, no per-record
sharing — those are ABAC/ReBAC and are YAGNI until a feature needs
them. No domains (multi-tenancy): one role set per user until a second
tenant exists. No deny-override or priority lines.
