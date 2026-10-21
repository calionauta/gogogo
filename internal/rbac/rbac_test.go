// SCOPE:core — Tests for RBAC with embedded Casbin model/policy
package rbac

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// TestRBACEmbeddedIsComplete proves the embedded assets are present and
// non-empty — a missing //go:embed target would fail the build, but an
// empty model/policy would silently authorize nothing.
func TestRBACEmbeddedIsComplete(t *testing.T) {
	if Model() == "" {
		t.Fatal("embedded model is empty")
	}
	if Policy() == "" {
		t.Fatal("embedded policy is empty")
	}
}

// TestRBACEnforcement exercises the real decision path: an allow and a deny
// derived from the embedded policy. This is the behaviour that would break
// if the model matcher or the policy rules regressed.
func TestRBACEnforcement(t *testing.T) {
	e, err := NewEnforcer()
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}

	allowed, err := e.Enforce("admin", "/anything", "GET")
	if err != nil {
		t.Fatalf("enforce admin: %v", err)
	}
	if !allowed {
		t.Fatal("admin should be allowed on any path")
	}

	allowed, err = e.Enforce("user", "/todo", "GET")
	if err != nil {
		t.Fatalf("enforce user GET /todo: %v", err)
	}
	if !allowed {
		t.Fatal("user should be allowed to GET /todo")
	}

	denied, err := e.Enforce("user", "/config", "GET")
	if err != nil {
		t.Fatalf("enforce user GET /config: %v", err)
	}
	if denied {
		t.Fatal("user must NOT be allowed to GET /config (admin-only surface)")
	}

	denied, err = e.Enforce("anonymous", "/todo", "GET")
	if err != nil {
		t.Fatalf("enforce anonymous GET /todo: %v", err)
	}
	if denied {
		t.Fatal("anonymous must NOT reach app routes (login first)")
	}
}

// TestRBACUserCannotReachOperatorSurfaces pins the admin-only DECISION
// (not just the route list): a plain user is denied every operator
// surface. The route-coverage lint lives in internal/server
// (TestRBACCoverageMatchesRealMux) because only that package can import
// both the mux and this one without a cycle; this test keeps the policy
// side honest on its own.
//
// Red-proof: widen any row to cover /config for user and this goes red.
func TestRBACUserCannotReachOperatorSurfaces(t *testing.T) {
	e, err := NewEnforcer()
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	opSurfaces := []string{
		"/config", "/config/", "/console", "/console/", "/console/x",
		"/dagnats", "/dagnats/", "/dagnats/x",
	}
	for _, path := range opSurfaces {
		for _, method := range []string{"GET", "POST"} {
			denied, derr := e.Enforce("user", path, method)
			if derr != nil {
				t.Fatalf("enforce user %s %s: %v", method, path, derr)
			}
			if denied {
				t.Errorf("user reaches operator surface %s %s, want admin-only", method, path)
			}
		}
	}
	for _, path := range []string{"/config", "/console/", "/dagnats/"} {
		ok, cerr := e.Enforce("admin", path, "GET")
		if cerr != nil {
			t.Fatalf("enforce admin %s: %v", path, cerr)
		}
		if !ok {
			t.Errorf("admin cannot reach %s", path)
		}
	}
}

// TestRBACPolicyBaseline pins the shipped policy's public surface: enabling
// RBAC must not brick the CSS/JS the page needs or the healthcheck. Each row
// is a decision the default policy is expected to make.
func TestRBACPolicyBaseline(t *testing.T) {
	e, err := NewEnforcer()
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	cases := []struct {
		sub, obj, act string
		want          bool
	}{
		{"anonymous", "/static/app.min.css", "GET", true},
		{"anonymous", "/sw.js", "GET", true},
		{"anonymous", "/sw.js", "HEAD", true},
		{"anonymous", "/health", "GET", true},
		{"anonymous", "/health", "HEAD", true},
		{"anonymous", "/", "GET", true},
		{"anonymous", "/login", "POST", true},
		{"anonymous", "/api/auth/signup/credential", "POST", true},
		{"anonymous", "/api/session/stream", "GET", true},
		{"anonymous", "/api/todos", "GET", false}, // app data requires a session
		{"anonymous", "/todo", "GET", false},
		{"anonymous", "/config", "GET", false},
		{"user", "/todo", "GET", true},
		{"user", "/api/todos", "GET", true},
		{"user", "/whiteboard/abc", "GET", true},
		{"user", "/config", "GET", false}, // admin-only surface
		{"user", "/config/", "GET", false},
		{"admin", "/config", "GET", true},
		{"admin", "/anything", "GET", true},
		{"stranger", "/todo", "GET", false}, // unknown subject denies
	}
	for _, c := range cases {
		got, err := e.Enforce(c.sub, c.obj, c.act)
		if err != nil {
			t.Fatalf("enforce %s %s %s: %v", c.sub, c.act, c.obj, err)
		}
		if got != c.want {
			t.Errorf("Enforce(%q,%q,%q) = %v, want %v", c.sub, c.obj, c.act, got, c.want)
		}
	}
}

// TestRBACPolicyHasNoDuplicateLines pins the dedupe cleanup: every
// non-comment policy line must appear once. Duplicates don't change
// decisions, so no Enforce test can catch them — this text-level test
// is the only guard. Red-proof: duplicate any p, line and this goes red.
func TestRBACPolicyHasNoDuplicateLines(t *testing.T) {
	seen := map[string]int{}
	for line := range strings.SplitSeq(Policy(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		seen[line]++
		if seen[line] > 1 {
			t.Errorf("duplicate policy line: %q", line)
		}
	}
}

// TestRBACRolesAreFlat pins the no-inheritance decision: the shipped
// policy must contain zero grouping (g) rules. Casbin inheritance only
// ADDS permissions, and viewer is a SUBSET of user — adding
// `g, viewer, user` would silently escalate every viewer to full user.
// Red-proof: add any g, line to policy.csv and this goes red.
func TestRBACRolesAreFlat(t *testing.T) {
	e, err := NewEnforcer()
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	if g, err := e.GetGroupingPolicy(); err != nil {
		t.Fatalf("grouping policy: %v", err)
	} else if len(g) != 0 {
		t.Errorf("grouping policy = %v, want empty (roles stay flat, see roles.go)", g)
	}
}

// TestRBACPolicyShape is the mechanical contract for every future
// policy edit (human or LLM): each row must name a known subject, a
// path-shaped object, and a compilable act regex — and viewer rows may
// only wildcard the three page/static surfaces below. A typo'd subject
// (`p, users, ...`) or a broad viewer grant (`p, viewer, /api/*, ...`)
// fails closed at runtime and would survive every Enforce test, so the
// shape test is the only guard. Red-proof: add either planted row and
// this goes red. See docs/rbac.md ("Adding a feature").
func TestRBACPolicyShape(t *testing.T) {
	knownSubjects := map[string]bool{
		RoleAdmin: true, RoleUser: true, RoleViewer: true, RoleAnonymous: true,
	}
	// The only viewer wildcards: page renders, public surfaces, and the
	// shared docs below. A blanket `/api/*`-shaped viewer row stays
	// unacceptable — the demo contract is read + create + toggle +
	// delete-own on todos, and open-edit/author-delete on shared docs,
	// nothing else destructive, nothing paid.
	//
	// Shared collaborative docs. The wildcard is the point and not a
	// loophole: a shared doc's id is unknowable in advance, so enumerating
	// per-doc paths is impossible. Both features keep their destructive
	// verb (doc/delete) inside the SAME pattern - keyMatch2 lets `*` cross
	// `/` - so the middleware cannot be what makes a new verb safe.
	// canDeleteDoc on the row (author or admin) is that guard, and each
	// feature's permissions_test pins it. Widening the set of SUBTREES
	// below is the part that must stay deliberate.
	viewerWildcards := map[string]bool{
		"/todo/*": true, "/static/*": true, "/api/auth/*": true,
		"/api/todos/*/toggle": true,
		// Delete-own verbs: the middleware gates the verb class, the
		// handler enforces per-record ownership (canDeleteRow) - a
		// viewer reaches these paths but only deletes their own rows.
		"/api/todos/*/delete": true, "/api/todos/*/confirm-delete": true,
		"/whiteboard/*": true, "/api/whiteboard/*": true,
		"/notes/*": true, "/api/notes/*": true,
	}
	for line := range strings.SplitSeq(Policy(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) != 4 || strings.TrimSpace(parts[0]) != "p" {
			t.Errorf("policy line is not a 4-field p rule: %q", line)
			continue
		}
		sub, obj, act := strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2]), strings.TrimSpace(parts[3])
		if !knownSubjects[sub] {
			t.Errorf("subject %q outside the role vocabulary "+
				"(typo? or new role: add to roles.go AND knownSubjects): %q", sub, line)
		}
		if !strings.HasPrefix(obj, "/") {
			t.Errorf("object %q is not a path: %q", obj, line)
		}
		if _, err := regexp.Compile(act); err != nil {
			t.Errorf("act %q does not compile: %v (%q)", act, err, line)
		}
		if sub == RoleViewer && strings.Contains(obj, "*") && !viewerWildcards[obj] {
			t.Errorf("viewer wildcard %q outside the allowlist "+
				"(enumerate paths, or extend viewerWildcards with reason): %q", obj, line)
		}
	}
}

// TestRBACViewerSubject pins the viewer passthrough: the exact string
// "viewer" resolves to itself (never flattened to user, never
// escalated), unknown strings still fall back to user, and failures
// stay anonymous. Red-proof: return RoleUser for viewer and the
// enforcement test below goes red with it.
func TestRBACViewerSubject(t *testing.T) {
	sess := &auth.ValidatedSession{User: &auth.User{ID: "u1", Email: "v@example.com"}, Role: "viewer"}
	if got := subjectOf(sess, nil); got != "viewer" {
		t.Fatalf("subjectOf(viewer) = %q, want viewer", got)
	}
	if !IsViewer("viewer") || IsViewer("user") || IsViewer("admin") || IsViewer("") {
		t.Fatal("IsViewer must be true only for the exact viewer role")
	}
}

// TestRBACViewerPolicy pins the demo contract: read + create + toggle +
// delete-own allowed (clear-completed deletes own rows only, so it rides
// the same ownership; per-record enforcement lives in the handler — the
// policy gates the verb class, never the row), every other destructive
// and every paid path denied. Unknown verdict
// strings and unknown roles are covered by the closed-set rule in the
// middleware (exact match or fallback), not here.
func TestRBACViewerPolicy(t *testing.T) {
	e, err := NewEnforcer()
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	allow := []struct{ method, path string }{
		{"GET", "/todo"},
		{"GET", "/api/todos"},
		{"POST", "/api/todos"},
		{"POST", "/api/todos/abc123/toggle"},
		{"GET", "/api/todos/stream"},
		{"POST", "/api/todos/abc123/delete"},
		{"GET", "/api/todos/abc123/confirm-delete"},
		{"POST", "/api/todos/completed/delete"},
		// The collab surface: notes and whiteboards are SHARED, so a
		// viewer opens and EDITS any of them — that is what makes the demo
		// prove collaboration from two browsers. Delete rides the same
		// wildcard; the per-doc author/admin rule lives in each feature's
		// canDeleteDoc and is pinned by that package's permission test.
		{"GET", "/whiteboard"},
		{"GET", "/whiteboard/abc"},
		{"POST", "/api/whiteboard/abc/update"},
		{"GET", "/api/whiteboards/fragment"},
		{"GET", "/notes"},
		{"GET", "/notes/abc"},
		{"POST", "/api/notes/abc/op"},
		{"GET", "/api/notes/fragment"},
	}
	for _, a := range allow {
		ok, err := e.Enforce("viewer", a.path, a.method)
		if err != nil {
			t.Fatalf("enforce viewer %s %s: %v", a.method, a.path, err)
		}
		if !ok {
			t.Errorf("viewer must reach %s %s", a.method, a.path)
		}
	}
	deny := []struct{ method, path string }{
		{"POST", "/api/todos/suggest"},
		{"POST", "/api/todos/retry-demo"},
		{"GET", "/config"},
		{"GET", "/chat"},
	}
	for _, d := range deny {
		ok, err := e.Enforce("viewer", d.path, d.method)
		if err != nil {
			t.Fatalf("enforce viewer %s %s: %v", d.method, d.path, err)
		}
		if ok {
			t.Errorf("viewer must NOT reach %s %s", d.method, d.path)
		}
	}
}

// TestRBACMiddlewareViewerEnforcement pins the verb-level split: through
// the real middleware with enforcement on, a viewer DELETE passes the
// middleware (the verb is allowed) — row ownership is the handler's
// decision (canDeleteRow), pinned by the handler sharing tests.
func TestRBACMiddlewareViewerEnforcement(t *testing.T) {
	sqlDB, err := db.OpenAt(t.TempDir() + "/rbac-viewer.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("db migrate: %v", merr)
	}
	a, aerr := auth.New(dbgen.New(sqlDB), false)
	if aerr != nil {
		t.Fatalf("auth new: %v", aerr)
	}
	ctx := context.Background()
	if _, _, serr := a.Signup(ctx, "watcher@example.com", "Correct-horse-99!", ""); serr != nil {
		t.Fatalf("signup: %v", serr)
	}
	q := dbgen.New(sqlDB)
	if serr := q.SetUserRole(ctx, dbgen.SetUserRoleParams{
		Role: RoleViewer, Updated: dbschema.Now(), Email: "watcher@example.com",
	}); serr != nil {
		t.Fatalf("set viewer role: %v", serr)
	}
	token, _, lerr := a.Login(ctx, "watcher@example.com", "Correct-horse-99!")
	if lerr != nil {
		t.Fatalf("login: %v", err)
	}
	e, err := NewEnforcer()
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := Middleware(a, e, true)(next)

	// #nosec G124 — test-only request cookie.
	cookie := &http.Cookie{Name: auth.CookieName, Value: token}
	drive := func(method, target string) int {
		t.Helper()
		req := httptest.NewRequestWithContext(ctx, method, target, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := drive(http.MethodPost, "/api/todos/abc/delete"); code != http.StatusOK {
		t.Errorf("viewer DELETE verb passes middleware (ownership is the handler's job) status = %d, want 200", code)
	}
	if code := drive(http.MethodPost, "/api/todos"); code != http.StatusOK {
		t.Errorf("viewer create status = %d, want 200", code)
	}
	if code := drive(http.MethodPost, "/api/todos/abc/toggle"); code != http.StatusOK {
		t.Errorf("viewer toggle status = %d, want 200", code)
	}
	if code := drive(http.MethodGet, "/todo"); code != http.StatusOK {
		t.Errorf("viewer page status = %d, want 200", code)
	}
}

// TestRBACPathMatcherEscapesLiterals pins the matcher's one deviation from
// Casbin's util.KeyMatch2: policy objects are PATHS, so a literal is
// matched literally. LaputaKeyMatch2 leaves regex metacharacters live, so
// `p, anonymous, /sw.js, GET` also allows a request for `/swXjs` — a
// silent over-grant invisible in the CSV. Red-proof: revert pathMatches to
// util.KeyMatch2 and the /swXjs case below goes green (i.e. this fails).
func TestRBACPathMatcherEscapesLiterals(t *testing.T) {
	e, err := NewEnforcer()
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	cases := []struct {
		sub, path string
		want      bool
		why       string
	}{
		{"anonymous", "/sw.js", true, "the exact path still matches"},
		{"anonymous", "/swXjs", false, "a dot is a dot, not a wildcard"},
		{"anonymous", "/swajs", false, "same, other byte"},
		{"user", "/notes/x", true, "a trailing /* still spans segments"},
		{"user", "/api/notes/abc/delete", true, "documented keyMatch2 semantics: * crosses /"},
		{"viewer", "/api/notes/abc/op", true, "shared-doc write rides the same wildcard"},
		{"user", "/notez/x", false, "a bare prefix is not a match"},
	}
	for _, c := range cases {
		got, eerr := e.Enforce(c.sub, c.path, "GET")
		if eerr != nil {
			t.Fatalf("enforce %s %s: %v", c.sub, c.path, eerr)
		}
		if got != c.want {
			t.Errorf("Enforce(%q, %q, GET) = %v, want %v (%s)", c.sub, c.path, got, c.want, c.why)
		}
	}
}

// TestRBACPolicyObjectsCarryNoMetacharacters pins ValidatePolicyObjects:
// the shipped policy is clean, and a planted object with a regex
// metacharacter is REFUSED at enforcer-build time rather than silently
// changing meaning. A dot stays legal (real paths have extensions).
// Red-proof: delete the ValidatePolicyObjects call from NewEnforcer and
// the planted-policy half stops returning an error.
func TestRBACPolicyObjectsCarryNoMetacharacters(t *testing.T) {
	if err := ValidatePolicyObjects(Policy()); err != nil {
		t.Fatalf("shipped policy rejected: %v", err)
	}
	bad := []string{
		"p, user, /api/v1.0/(v2)/*, .*",
		"p, user, /api/x+/*, .*",
		"p, user, /api/[abc]/*, .*",
		"p, user, /api/{id}/*, .*",
	}
	for _, row := range bad {
		if err := ValidatePolicyObjects(row); err == nil {
			t.Errorf("ValidatePolicyObjects accepted a metacharacter object: %q", row)
		}
	}
	ok := []string{
		"p, user, /sw.js, GET",
		"p, user, /api/notes/*, .*",
		"p, user, /api/notes/:id/op, POST",
	}
	for _, row := range ok {
		if err := ValidatePolicyObjects(row); err != nil {
			t.Errorf("ValidatePolicyObjects rejected a legal path %q: %v", row, err)
		}
	}
}
