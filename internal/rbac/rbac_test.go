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

	allowed, err = e.Enforce("user", "/profile", "GET")
	if err != nil {
		t.Fatalf("enforce user GET /profile: %v", err)
	}
	if !allowed {
		t.Fatal("user should be allowed to GET /profile")
	}

	denied, err := e.Enforce("user", "/admin", "POST")
	if err != nil {
		t.Fatalf("enforce user POST /admin: %v", err)
	}
	if denied {
		t.Fatal("user must NOT be allowed to POST /admin")
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
		{"anonymous", "/api/todos", "GET", false}, // app data requires a session
		{"user", "/profile", "GET", true},
		{"user", "/admin", "POST", false},
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
