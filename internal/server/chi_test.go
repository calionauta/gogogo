// SCOPE:core — Flat-stack boot tests (red-proof for the serve seam).
package server

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/config"
	todochi "github.com/calionauta/gogogo/features/todo/chi"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/queue"
)

func TestStartChiDisabled(t *testing.T) {
	cfg := config.Load()
	cfg.ChiAddr = ""
	stop, err := StartChi(cfg, nil)
	if err != nil {
		t.Fatalf("disabled StartChi err = %v, want nil", err)
	}
	if stop != nil {
		t.Fatal("disabled StartChi returned a stop func, want nil")
	}
}

// TestStartChiBoot proves the seam boots end to end: Turso opened,
// migrations applied, listener up — and stop shuts it down cleanly.
// Port :0 lets the kernel pick a free port (no fixed-port flakes).
func TestStartChiBoot(t *testing.T) {
	cfg := config.Load()
	cfg.ChiAddr = "127.0.0.1:0"
	cfg.Dev = true
	cfg.Turso.DBPath = t.TempDir() + "/boot.db"

	stop, err := StartChi(cfg, nil)
	if err != nil {
		t.Fatalf("StartChi err = %v", err)
	}
	if stop == nil {
		t.Fatal("StartChi returned nil stop, want a shutdown func")
	}
	defer stop()

	var versions []string
	sqlDB, err := sql.Open("turso", cfg.Turso.DBPath+"?_busy_timeout=10000")
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer sqlDB.Close()
	rows, err := sqlDB.QueryContext(context.Background(), `SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatalf("read migrations: %v (seam did not migrate)", err)
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan: %v", err)
		}
		versions = append(versions, v)
	}
	if len(versions) == 0 || versions[len(versions)-1] != "0004_flat_identity" {
		t.Fatalf("migrations = %v, want ledger through 0004_flat_identity", versions)
	}
}

// TestBuildChiMux proves the served composition routes: the exact mux
// StartChi serves, driven over HTTP (this is the composition that broke
// once under chi.Mount at the root).
func TestBuildChiMux(t *testing.T) {
	sqlDB, err := db.OpenAt(t.TempDir() + "/mux.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("db migrate: %v", err)
	}
	a, aerr := auth.New(dbgen.New(sqlDB), false)
	if aerr != nil {
		t.Fatalf("auth new: %v", aerr)
	}
	todoH := todochi.NewHandler(dbgen.New(sqlDB), a, queue.NewSSEHub(), "daisyui")

	mux := buildChiMux(a, todoH)
	body := `{"email":"mux@example.com","password":"Correct-horse-99!"}`
	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/auth/signup/credential", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("mux signup status = %d (%s)", rec.Code, rec.Body.String())
	}
	found := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("mux signup set no %q cookie", auth.CookieName)
	}
}
