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
- **`internal/rbac/dynamic.go`** — the matcher: `pathMatch` (the safe
  object→regexp compiler, see "Policy row anatomy"), `attrOK` (the
  ownership clause), the ABAC `Object`, and `ValidatePolicyObjects` (the
  metacharacter guard that runs at enforcer build).
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
matches (`pathMatch`), the method matches (`regexMatch`: `(GET|POST)` or
`.*`), and `attrOK` passes. Effect: any allow wins. No `g,` rows ship —
roles are flat (see below).

`pathMatch` keeps `keyMatch2`'s SHAPE (`*` spans `/`, `:name` matches one
segment) but **escapes every literal**, which Casbin's `util.KeyMatch2`
does not: upstream rewrites `/*` to `/.*` and leaves `.`, `+`, `(` live
as regex, so `p, anonymous, /sw.js, GET` also allowed `/swXjs`. Escaping
makes an object mean exactly what it reads. `ValidatePolicyObjects` then
refuses an object containing a regex metacharacter (a `.` is allowed —
real paths have extensions), so an author cannot silently write a
pattern where they meant a path. Both run at enforcer build:
`NewEnforcer` and `NewEnforcerWithAdapter` (the DB source included —
policy in a table is an editable privilege surface).

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
  `/api/auth/*`, `/api/todos/*/toggle`, `/api/todos/*/delete`,
  `/api/todos/*/confirm-delete`, `/whiteboard/*`, `/api/whiteboard/*`,
  `/notes/*`, `/api/notes/*` fail the shape test. Delete verbs are
  verb-class grants — row ownership stays in the handler.
- **Collaborative docs are shared, and their viewer rows are wildcards on
  purpose.** Notes and whiteboards are not per-owner: every signed-in user
  opens and edits every doc, so enumerating doc paths is impossible. The
  destructive half is enforced per doc by each feature's `canDeleteDoc`
  (author or admin): the policy grants the verb class, the handler decides
  the row. A viewer therefore edits demo1's note and is refused a delete on
  it — the two-browser demo, not a leak.
- **A shared-doc wildcard covers its own delete verb, and that is fine.**
  Casbin's `keyMatch2` lets `*` cross `/`, so `/api/notes/*` already
  matches `/api/notes/{id}/delete`. The wildcard therefore cannot be what
  keeps a new verb safe; the per-doc check is. The part that must stay
  deliberate is the set of SUBTREES (the shape test's allowlist), because
  widening that is what actually grants a new surface.
- **Fail-closed by construction**: a route with no row denies everyone
  but admin. Adding a route without adding its rows is a 403 bug, and
  the coverage test names it.

## Adding a feature (checklist)

The two failure modes are a route with **no** row (fail-closed 403) and a
route that only *admin* reaches because `p, admin, /*, .*` masked the
missing row. Both are caught mechanically, so follow the list and let the
tests confirm it.

1. Mount routes in `internal/server/chi.go` — add the handler to
   `featureMounts` (one line) and, if the feature needs its own handler
   parameter, to `buildChiMux`. Every feature router is a `chi.Router`,
   which is what lets the coverage test walk it.
2. Add `p, user, /prefix, .*` + `p, user, /prefix/*, .*` in the user block.
3. Add enumerated viewer rows only if the feature has a viewer-safe
   surface (read/create/toggle class, plus delete-own — never shared
   deletes, never paid).
4. If the route must stay **admin-only**, add its pattern to
   `adminOnlyRoutes` in `internal/server/rbac_coverage_test.go`. That map
   is the single place the decision is written down; without it the
   coverage test demands a `user` row.
5. If the route is **public**, add it to `publicRoutes` in the same file
   and add an `anonymous` row.
6. Run `go test -count=1 ./internal/rbac/ ./internal/server/` (~15s; the
   `rbac-policy` pre-commit hook runs this subset when the commit touches
   `internal/rbac/**` or `internal/server/chi*.go`).
7. Roll out with `RBAC_PERMISSIVE=true` for one deploy, read the
   `rbac: permissive allow (would deny)` lines, add the missing rows,
   then turn it off. Production posture is permissive=false.

**Why the coverage test is strict.** It asserts the policy allows
`user` **on its own** for every non-admin-only route, never
`user || admin`. The weaker form hides the exact bug that shipped once:
`/api/stream/status` had no `user` row, admin reached it through `/*`,
nothing looked wrong — and every signed-in non-admin got a 403 on the
probe `sse-open.js` polls, which made it report a healthy stream as dead
and rethrow. A public route still needs its `user` row for the same
reason (a session must not rank *below* anonymous).

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
- **Operator surfaces** (`TestRBACUserCannotReachOperatorSurfaces`):
  `/config`, `/console`, `/dagnats` denied for user on every method, still
  reachable for admin.
- **Viewer matrix + middleware**: allow/deny decisions plus a real
  viewer through the real middleware (create/toggle/delete-own pass at
  the verb; row ownership denies in the handler).
- **Display** (`TestTodoViewerDeleteAffordanceDisabled`,
  `TestTodoSuggestTabVisibility`): the UI narrates the role.
- **Boot wiring** (`TestApplyRBACWiresPermissive`,
  `TestPromoteViewersPinsViewerContract`): flags and promotion reach
  the middleware.
- **Matcher** (`TestRBACPathMatcherEscapesLiterals`): a literal dot stays
  a dot, and `*` still spans `/` — the two halves of the safe matcher.
- **Object validation** (`TestRBACPolicyObjectsCarryNoMetacharacters`):
  the shipped policy is clean and a planted regex object is refused.
- **Realtime probe** (`TestRBACStreamStatusStaysReachable`): every subject
  reaches `/api/stream/status`, because `sse-open.js` treats a 403 as a
  dead stream and rethrows.
- **Coverage from the source** (`TestRBACCoverageMatchesRoutes`,
  `TestRBACPublicRoutesArePublic` in this package): scans every
  `r.Get/Post/...` in `features/*/handlers/chi.go` plus the core mounts in
  `internal/server/`, then demands a DIRECT `user` row per routed method.
  Reading source instead of importing handlers keeps this package free of
  feature dependencies, so the check survives any trimmed tree (the
  installer drops whole features) with no manifest line. This replaced the
  hand-written path list that let `/api/stream/status` ship unguarded.
- **Anonymous access** (`TestNotesRejectsAnonymousAccess`,
  `TestWhiteboardRejectsAnonymousAccess` + their signed-in counterparts):
  read/write/presence without a session is 401 and persists nothing.
- **Write contract** (`TestNotesRefusesWriteToUnregisteredDoc`,
  `TestWhiteboardRefusesDrawOnUnregisteredDoc`): a write to an id with no
  metadata row is 404 and persists nothing, using the production registry
  (the transport fixtures' opt-out is not in play).

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
`Path`, `SubjectID` and the client `IP`, always with `Owner` EMPTY.

**No shipped handler fills `Owner`.** The seam is wired, pinned by
`TestRBACABACOwnership`, and unused: per-record decisions live in
`canDeleteDoc`/`canDeleteRow`, which read the row the handler already
loaded. Reach for the Object form when a per-record decision must happen
BEFORE the handler runs (a middleware-level row check); filling it inside a
handler that already has the row adds a second place for the same rule.
Add a tenant or IP rule by extending `attrsOK` - no model change needed.

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

## Authentication is not authorization (read before trusting RBAC)

`RBAC_ENABLED` defaults to **false**, so on a stock deployment the
middleware decides nothing. That makes the handler the ONLY guard, and a
policy row is not a substitute for one. Two rules follow:

- **A handler that reads or writes user data checks the session itself.**
  `sess == nil` answers 401, before touching a doc, a row or the body. Do
  not rely on "the policy grants /api/x/* to user" - with RBAC off that
  row is inert.
- **An `anonymous` row is a deliberate publication.** Add one only for a
  surface that is safe with no session at all (the landing page, assets,
  `/health`, the SSE open probe). A wildcard that happens to include a
  private route is how a surface goes public by accident.

The shipped restore points: notes and whiteboards gate the op/update
paths, both streams, presence and the board snapshot on a session
(`handleOp`, `handleStream`, `handleTyping`, `handleCaret`,
`handleUpdate`, `handlePresence`, `handleSnapshot`). The acceptance tests
are `TestNotesRejectsAnonymousAccess` and
`TestWhiteboardRejectsAnonymousAccess`; each asserts the write is refused
AND that no snapshot was persisted, so a regression cannot pass by
writing first and answering 401 after.

Shared does not mean public. Both features are shared among *signed-in*
users - that is what makes the two-browser demo prove collaboration - and
neither is readable by a signed-out client.

## The write contract: a doc must exist before its body changes

Both write paths - `POST /api/notes/{id}/op` and
`POST /api/whiteboard/{id}/update` - refuse an id with no metadata row
(404, nothing persisted). Without the check a session that invented an id
got a 200 and wrote a snapshot into `doc_snapshots` that no list ever
showed: an orphan costing storage, resurrectable by anyone who guessed the
id again.

**How the rule is wired, and why it is a field.** The write path asks
`registeredDoc`, which reads the `notes`/`boards` table. That is a FIELD on
the Handler (`registry docRegistry`), not a direct queries call, so a
transport fixture can opt out with one explicit line instead of rewriting
35 synthetic ids. Production leaves it nil, which means "ask the table".

The fixture opt-out is a Go field assignment in a `_test.go` file - there
is no env flag and no test-name sniffing, so a request cannot reach the
bypass. A test that wants the real contract simply does not set it:

- `TestNotesRefusesWriteToUnregisteredDoc` and
  `TestWhiteboardRefusesDrawOnUnregisteredDoc` assert 404 AND that no
  snapshot was persisted, then assert the registered path still writes.
- Every `port_*_test.go` fixture sets `h.registry` to `true` at its
  construction site, with a comment naming the transport reason.

If you add a feature on the same DocStore pattern, copy the pair: a
`registeredDoc`-style check on the write path, the field, the fixture
opt-out, and the acceptance test that uses the default.

## What RBAC does not do

- **No per-record sharing / ReBAC** — per-record decisions ship as
  `canDeleteDoc`/`canDeleteRow` + the registry check, not as policy rows.
  Sharing ONE record between named users (a per-doc ACL) is still a
  per-feature job: there is no table for it and no matcher reads one.
- **No domains** (multi-tenancy): one role set per user until a second
  tenant exists. `Object.Tenant` is a reserved hook.
- **No deny-override or priority lines**.
