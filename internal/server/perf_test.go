// SCOPE:core — Runtime dashboard fence tests (red-proof).
package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/ui"
)

// TestPerfFence pins the three answers the operator surface must give:
// anonymous 401 (no session), admin but not in ADMIN_EMAILS 403, operator
// 200. Red-proof: drop RequireSession and the anonymous case goes 200;
// drop operatorGate and the non-operator case goes 200.
func TestPerfFence(t *testing.T) {
	ctx := context.Background()
	sqlDB, err := db.OpenAt(t.TempDir() + "/perf-fence.db")
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
	_, opTok, serr := a.Signup(ctx, "op@example.com", "Correct-horse-99!", "")
	if serr != nil {
		t.Fatalf("signup operator: %v", serr)
	}
	_, bossTok, serr := a.Signup(ctx, "boss@example.com", "Correct-horse-99!", "")
	if serr != nil {
		t.Fatalf("signup boss: %v", serr)
	}
	q := dbgen.New(sqlDB)
	promoteTestAdmin(t, q, "op@example.com")
	promoteTestAdmin(t, q, "boss@example.com")

	cfg := &config.Config{PerfEnabled: true, AdminEmails: []string{"op@example.com"}}
	mux := NewRouter()
	var shutdown []func()
	h := bootPerf(mux, a, cfg, &shutdown)
	if h == nil {
		t.Fatal("bootPerf returned nil with PERF_ENABLED on")
	}
	t.Cleanup(func() { closePerf(h) })

	get := func(tok string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/debug/statsviz/", nil)
		if tok != "" {
			// #nosec G124 — test-only request cookie; never traverses a network.
			req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	if rec := get(""); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous status = %d, want 401", rec.Code)
	}
	if rec := get(bossTok); rec.Code != http.StatusForbidden {
		t.Errorf("admin outside ADMIN_EMAILS status = %d, want 403", rec.Code)
	}
	rec := get(opTok)
	if rec.Code != http.StatusOK {
		t.Fatalf("operator status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Statsviz") {
		t.Error("operator body is not the statsviz page")
	}
}

// TestPerfDisabledMountsNothing pins the flag's off state: no handler, no
// collector goroutine, and the path stays a 404.
func TestPerfDisabledMountsNothing(t *testing.T) {
	mux := NewRouter()
	if h := bootPerf(mux, nil, &config.Config{}, nil); h != nil {
		t.Fatal("bootPerf returned a handler with PERF_ENABLED off")
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/debug/statsviz/", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("disabled dashboard status = %d, want 404", rec.Code)
	}
}

// TestPerfPageMiddlewareSeedsOnlyPages pins the shell flag: a page navigation
// carries it, an API request does not (the shell never renders there), and a
// disabled dashboard seeds nothing.
func TestPerfPageMiddlewareSeedsOnlyPages(t *testing.T) {
	seen := func(cfg *config.Config, path string) bool {
		t.Helper()
		var got bool
		h := perfPageMiddleware(cfg)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			got = ui.Perf(r.Context())
		}))
		h.ServeHTTP(httptest.NewRecorder(),
			httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil))
		return got
	}
	on := &config.Config{PerfEnabled: true}
	if !seen(on, "/todo") {
		t.Error("page request did not carry the perf flag")
	}
	if seen(on, "/api/todos") {
		t.Error("API request carried the perf flag")
	}
	if seen(&config.Config{}, "/todo") {
		t.Error("disabled dashboard seeded the perf flag")
	}
}
