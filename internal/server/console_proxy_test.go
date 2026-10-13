// SCOPE:core — Console proxy tests (red-proof).
package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// TestConsoleProxyForwardsPinsPath pins the passthrough contract:
// upstream absolute paths survive untouched (no prefix rewriting), the
// query survives, and the session email rides X-Forwarded-User.
// Red-proof: strip the prefix in the proxy and the path assertion goes
// red; drop the header stamp and the user assertion goes red.
func TestConsoleProxyForwardsPinsPath(t *testing.T) {
	ctx := context.Background()
	sqlDB, err := db.OpenAt(t.TempDir() + "/console-fwd.db")
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
	_, userTok, serr := a.Signup(ctx, "fwd@example.com", "Correct-horse-99!", "")
	if serr != nil {
		t.Fatalf("signup: %v", serr)
	}
	var seenPath, seenUser string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.RequestURI()
		seenUser = r.Header.Get("X-Forwarded-User")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(backend.Close)

	mux := http.NewServeMux()
	cfg := &config.Config{}
	cfg.DagNats.Enabled = true
	cfg.DagNats.HTTPAddr = backend.Listener.Addr().String()
	mountConsoleProxy(mux, a, cfg)

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/console/triggers?x=1", nil)
	// #nosec G124 — test-only request cookie; never traverses a network.
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: userTok})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("proxied status = %d, want 200", rec.Code)
	}
	if seenPath != "/console/triggers?x=1" {
		t.Errorf("backend saw path %q, want untouched /console/triggers?x=1", seenPath)
	}
	if seenUser != "fwd@example.com" {
		t.Errorf("backend saw X-Forwarded-User %q, want the session email", seenUser)
	}
}

// TestConsoleProxyRequiresSession pins the guest fence: no cookie, no
// proxy (401), and the backend is never touched. Red-proof: mount the
// proxy without RequireSession and this goes red (backend hit, 200).
func TestConsoleProxyRequiresSession(t *testing.T) {
	ctx := context.Background()
	hits := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(backend.Close)

	sqlDB, err := db.OpenAt(t.TempDir() + "/console-guest.db")
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
	mux := http.NewServeMux()
	cfg := &config.Config{}
	cfg.DagNats.Enabled = true
	cfg.DagNats.HTTPAddr = backend.Listener.Addr().String()
	mountConsoleProxy(mux, a, cfg)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/console/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("guest /console/ status = %d, want 401", rec.Code)
	}
	if hits != 0 {
		t.Errorf("backend hits = %d, want 0 (guest never proxied)", hits)
	}
}

// TestConsoleProxyDisabledPinsAbsence pins the opt-out: engine off
// means no mount (404), never a 502 to a dead address. Red-proof:
// mount unconditionally and this goes red.
func TestConsoleProxyDisabledPinsAbsence(t *testing.T) {
	sqlDB, err := db.OpenAt(t.TempDir() + "/console-off.db")
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
	mux := http.NewServeMux()
	cfg := &config.Config{}
	cfg.DagNats.Enabled = false
	mountConsoleProxy(mux, a, cfg)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/console/", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("disabled /console/ status = %d, want 404", rec.Code)
	}
}

// TestConsoleBareRedirect pins /console → /console/ so the menu link
// never depends on the trailing slash. Only the redirect is asserted
// here (no session needed for a redirect — the gate lives on /console/).
func TestConsoleBareRedirect(t *testing.T) {
	sqlDB, err := db.OpenAt(t.TempDir() + "/console-redir.db")
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
	mux := http.NewServeMux()
	cfg := &config.Config{}
	cfg.DagNats.Enabled = true
	cfg.DagNats.HTTPAddr = "127.0.0.1:1"
	mountConsoleProxy(mux, a, cfg)

	for _, tc := range []struct{ from, to string }{
		{"/console", "/console/"},
		{"/dagnats", "/dagnats/"},
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, tc.from, nil))
		if rec.Code != http.StatusMovedPermanently {
			t.Errorf("%s status = %d, want 301", tc.from, rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != tc.to {
			t.Errorf("%s Location = %q, want %q", tc.from, loc, tc.to)
		}
	}
}

// TestConsoleAliasRerootsPinsBrand pins the /dagnats/ alias: the branded
// entry strips its prefix and lands on the upstream /console/ tree,
// keeping the DagNats name in the address bar. Red-proof: mount the
// alias as an identity proxy and the path assertion goes red (backend
// would see /dagnats/..., which upstream does not serve).
func TestConsoleAliasRerootsPinsBrand(t *testing.T) {
	ctx := context.Background()
	sqlDB, err := db.OpenAt(t.TempDir() + "/console-alias.db")
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
	_, userTok, serr := a.Signup(ctx, "alias@example.com", "Correct-horse-99!", "")
	if serr != nil {
		t.Fatalf("signup: %v", serr)
	}
	var seenPath string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.RequestURI()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(backend.Close)

	mux := http.NewServeMux()
	cfg := &config.Config{}
	cfg.DagNats.Enabled = true
	cfg.DagNats.HTTPAddr = backend.Listener.Addr().String()
	mountConsoleProxy(mux, a, cfg)

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/dagnats/triggers?x=1", nil)
	// #nosec G124 — test-only request cookie; never traverses a network.
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: userTok})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("aliased status = %d, want 200", rec.Code)
	}
	if seenPath != "/console/triggers?x=1" {
		t.Errorf("backend saw path %q, want rerooted /console/triggers?x=1", seenPath)
	}
}
