// SCOPE:core — Tests for RBAC with embedded Casbin model/policy
package rbac

import "testing"

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

// TestRBACPolicyCoversMuxRoutes pins policy against the real mux: every
// subtree buildChiMux mounts must decide allow for user or admin — never
// silent-deny. Adding a feature means adding its paths here AND in
// policy.csv (fail-closed by construction: an unlisted route denies).
// Mirror: internal/server/chi.go buildChiMux.
func TestRBACPolicyCoversMuxRoutes(t *testing.T) {
	e, err := NewEnforcer()
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	userOK := []string{
		"/todo", "/todo/", "/api/todos", "/api/todos/",
		"/login", "/logout", "/whiteboard", "/whiteboard/",
		"/api/whiteboard", "/api/whiteboard/", "/api/whiteboards/",
		"/notes", "/notes/", "/api/notes", "/api/notes/",
		"/room", "/room/", "/chat", "/chat/", "/api/chat/",
		"/genui", "/genui/", "/api/genui/",
		"/api/credits", "/api/credits/", "/api/ai/", "/api/byok/",
		"/api/onboarding", "/api/onboarding/",
		"/api/auth/signup/credential", "/api/session",
		"/static/app.min.css", "/sw.js", "/health", "/",
	}
	for _, path := range userOK {
		allowUser, err := e.Enforce("user", path, "GET")
		if err != nil {
			t.Fatalf("enforce user %s: %v", path, err)
		}
		allowAdmin, err := e.Enforce("admin", path, "GET")
		if err != nil {
			t.Fatalf("enforce admin %s: %v", path, err)
		}
		if !allowUser && !allowAdmin {
			t.Errorf("route %q denied for user and admin — policy gap", path)
		}
	}
	for _, path := range []string{"/config", "/config/"} {
		allowUser, err := e.Enforce("user", path, "GET")
		if err != nil {
			t.Fatalf("enforce user %s: %v", path, err)
		}
		if allowUser {
			t.Errorf("route %q allowed for user, want admin-only", path)
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
		{"anonymous", "/health", "GET", true},
		{"anonymous", "/", "GET", true},
		{"anonymous", "/login", "POST", true},
		{"anonymous", "/api/auth/signup/credential", "POST", true},
		{"anonymous", "/api/session", "GET", true},
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
