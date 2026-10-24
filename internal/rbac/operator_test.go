// SCOPE:core — Operator-surface fence red-proof.
package rbac

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

func TestRequireAdminIgnoresRBACFlag(t *testing.T) {
	ctx := context.Background()
	sqlDB, err := db.OpenAt(t.TempDir() + "/operator.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("migrate: %v", merr)
	}
	q := dbgen.New(sqlDB)
	a, aerr := auth.New(q, false)
	if aerr != nil {
		t.Fatalf("auth: %v", aerr)
	}
	_, userTok, err := a.Signup(ctx, "user@example.com", "Correct-horse-99!", "")
	if err != nil {
		t.Fatalf("signup user: %v", err)
	}
	_, adminTok, err := a.Signup(ctx, "admin@example.com", "Correct-horse-99!", "")
	if err != nil {
		t.Fatalf("signup admin: %v", err)
	}
	if err := q.SetUserRole(ctx, dbgen.SetUserRoleParams{
		Role: RoleAdmin, Updated: dbschema.Now(), Email: "admin@example.com",
	}); err != nil {
		t.Fatalf("promote: %v", err)
	}

	h := RequireAdmin(a)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	cases := []struct {
		name  string
		token string
		want  int
	}{
		{"anonymous", "", http.StatusForbidden},
		{"user", userTok, http.StatusForbidden},
		{"admin", adminTok, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/config", nil)
			if tc.token != "" {
				// #nosec G124 — test-only request cookie.
				req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tc.token})
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}
