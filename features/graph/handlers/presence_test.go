package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	graphservice "github.com/calionauta/gogogo/internal/graph"
)

// graphMux mounts auth + graph handlers over a real temp DB so
// graphSignup produces a session cookie the handler accepts.
func graphMux(a *auth.Auth, h *Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Routes())
	mux.Handle("/", h.Routes())
	return mux
}

// nilAuth is a helper for tests that only need the mux shape.
func graphAuth(t *testing.T) *auth.Auth {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/graph-presence.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("db migrate: %v", merr)
	}
	a, err := auth.New(dbgen.New(sqlDB), false)
	if err != nil {
		t.Fatalf("auth new: %v", err)
	}
	return a
}

type fakePresence struct{ ro graphservice.Roster }

func (f fakePresence) Roster(context.Context, string) (graphservice.Roster, error) {
	return f.ro, nil
}

func TestHandlerPresenceShowsMembers(t *testing.T) {
	a := graphAuth(t)
	h := NewHandler(a, config.Load())
	h.SetWorkflowDefs([]string{workflowDef})
	h.SetPresenceReader(fakePresence{ro: graphservice.Roster{
		Members: []graphservice.Member{
			{Name: "ana@example.com", LastSeen: time.Now()},
		},
		Presenter: "ana@example.com",
	}})
	mux := graphMux(a, h)
	cookie := graphSignup(t, mux)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/graph/presence?room=lobby", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var doc graphservice.Document
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Source != "presence" {
		t.Fatalf("source = %q", doc.Source)
	}
	found := false
	for _, n := range doc.Nodes {
		if n.Label == "ana@example.com" && n.Meta == "presenter" {
			found = true
		}
	}
	if !found {
		t.Fatalf("presenter member missing: %+v", doc.Nodes)
	}
}

func TestHandlerPresenceDegradesWhenActorOff(t *testing.T) {
	a2 := graphAuth(t)
	h2 := NewHandler(a2, config.Load())
	h2.SetWorkflowDefs([]string{workflowDef})
	mux := graphMux(a2, h2)
	cookie := graphSignup(t, mux)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/graph/presence", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 degrade", rec.Code)
	}
	var doc graphservice.Document
	_ = json.Unmarshal(rec.Body.Bytes(), &doc)
	if doc.Source != "presence:unavailable" {
		t.Fatalf("source = %q, want degraded", doc.Source)
	}
	if !strings.Contains(rec.Body.String(), "unavailable") {
		t.Fatal("body missing unavailable")
	}
}
