// SCOPE:core — Operator seeding for the shell's account menu.
package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/rbac"
	"github.com/calionauta/gogogo/internal/ui"
)

// stubEngine is the one method the middleware needs from the workflow engine.
type stubEngine struct{ wired bool }

func (e stubEngine) Wired() bool { return e.wired }

// TestOperatorPageMiddlewareSeedsOnlyOperators pins the one fact the account
// menu reads. The console fence is "an engine is wired" AND admin AND an
// ADMIN_EMAILS match, so a listed admin in a binary without the engine must not
// be seeded (the console mount stays as the scaffold anchor but its upstream is
// gone: the link would land on a 502), an admin who is not listed must not be
// seeded (403), and a non-page request must not pay the session read at all.
func TestOperatorPageMiddlewareSeedsOnlyOperators(t *testing.T) {
	sqlDB, err := db.OpenAt(filepath.Join(t.TempDir(), "operator.db"))
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("migrate: %v", merr)
	}
	q := dbgen.New(sqlDB)
	a, aerr := auth.New(q, false)
	if aerr != nil {
		t.Fatalf("auth: %v", aerr)
	}
	_, opTok, err := a.Signup(context.Background(), "ops@example.com", "Correct-horse-99!", "")
	if err != nil {
		t.Fatalf("signup operator: %v", err)
	}
	_, adminTok, err := a.Signup(context.Background(), "admin@example.com", "Correct-horse-99!", "")
	if err != nil {
		t.Fatalf("signup admin: %v", err)
	}
	for _, email := range []string{"ops@example.com", "admin@example.com"} {
		if err := q.SetUserRole(context.Background(), dbgen.SetUserRoleParams{
			Role: rbac.RoleAdmin, Updated: dbschema.Now(), Email: email,
		}); err != nil {
			t.Fatalf("promote %s: %v", email, err)
		}
	}

	cfg := &config.Config{AdminEmails: []string{"ops@example.com"}}
	seeded := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if got, want := ui.Operator(r.Context()), r.URL.Query().Get("want") == "1"; got != want {
			t.Errorf("%s: Operator = %v, want %v", r.URL.Path, got, want)
		}
	})
	h := operatorPageMiddleware(cfg, a, stubEngine{wired: true})(seeded)

	cases := []struct {
		name   string
		token  string
		path   string
		want   string
		method string
	}{
		{"listed operator", opTok, "/todo", "1", http.MethodGet},
		{"admin not listed", adminTok, "/todo", "0", http.MethodGet},
		{"guest", "", "/", "0", http.MethodGet},
		{"api call is not a page", opTok, "/api/notifications/badge", "0", http.MethodGet},
	}
	// A trimmed binary keeps DAGNATS_ENABLED's default but has no engine to
	// answer: the entry must stay hidden rather than link to a 502.
	noEngine := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if ui.Operator(r.Context()) {
			t.Error("seeded an operator in a binary with no wired engine")
		}
	})
	notWired := operatorPageMiddleware(cfg, a, stubEngine{})(noEngine)
	trimmed := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/todo", nil)
	// #nosec G124 — test-only request cookie.
	trimmed.AddCookie(&http.Cookie{Name: auth.CookieName, Value: opTok})
	notWired.ServeHTTP(httptest.NewRecorder(), trimmed)

	for _, tc := range cases {
		t.Run(tc.name, func(_ *testing.T) {
			req := httptest.NewRequestWithContext(context.Background(), tc.method, tc.path+"?want="+tc.want, nil)
			if tc.token != "" {
				// #nosec G124 — test-only request cookie.
				req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tc.token})
			}
			h.ServeHTTP(httptest.NewRecorder(), req)
		})
	}
}
