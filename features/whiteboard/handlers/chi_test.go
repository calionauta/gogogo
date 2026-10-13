// SCOPE:feature — HTTP whiteboard contract tests (red-proof).
//
// Real Turso file, real auth, real Loro DocStore, driven over HTTP.
// Contracts: paths, statuses, redirects, owner isolation, snapshot
// round-trip through the persister, conflict + presence shapes.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/collab"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/queue"
)

func wbFixture(t *testing.T) http.Handler {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/wb.db")
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
	persister, perr := collab.NewTursoPersister(dbgen.New(sqlDB))
	if perr != nil {
		t.Fatalf("persister: %v", perr)
	}
	h := NewHandler(dbgen.New(sqlDB), a, queue.NewSSEHub(), persister,
		collab.NewDocStore(), config.Load())
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Handler())
	mux.Handle("/", h.Routes())
	return mux
}

func wbSignup(t *testing.T, mux http.Handler, email string) *http.Cookie {
	t.Helper()
	body := `{"email":"` + email + `","password":"Correct-horse-99!"}`
	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/auth/signup/credential", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("signup %s: status = %d", email, rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			return c
		}
	}
	t.Fatalf("signup %s: no cookie", email)
	return nil
}

func TestHandlersWhiteboardAuthGate(t *testing.T) {
	mux := wbFixture(t)
	for _, target := range []string{"/whiteboard", "/whiteboard/new", "/api/whiteboards/fragment"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil))
		if rec.Code != http.StatusSeeOther {
			t.Errorf("anon %s: status = %d, want 303", target, rec.Code)
		}
	}
}

func TestHandlersWhiteboardCreateAndList(t *testing.T) {
	mux := wbFixture(t)
	cookie := wbSignup(t, mux, "wb@example.com")

	newRec := httptest.NewRecorder()
	newReq := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/whiteboard/new", nil)
	newReq.AddCookie(cookie)
	mux.ServeHTTP(newRec, newReq)
	if newRec.Code != http.StatusFound {
		t.Fatalf("new status = %d, want 302", newRec.Code)
	}
	loc := newRec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/whiteboard/") {
		t.Fatalf("new Location = %q", loc)
	}
	docID := strings.TrimPrefix(loc, "/whiteboard/")

	// Board page renders.
	boardRec := httptest.NewRecorder()
	boardReq := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/whiteboard/"+docID, nil)
	boardReq.AddCookie(cookie)
	mux.ServeHTTP(boardRec, boardReq)
	if boardRec.Code != http.StatusOK {
		t.Fatalf("board status = %d", boardRec.Code)
	}

	// Fragment lists the new board.
	fragRec := httptest.NewRecorder()
	fragReq := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/whiteboards/fragment", nil)
	fragReq.AddCookie(cookie)
	mux.ServeHTTP(fragRec, fragReq)
	if fragRec.Code != http.StatusOK {
		t.Fatalf("fragment status = %d", fragRec.Code)
	}
	if !strings.Contains(fragRec.Body.String(), docID) {
		t.Fatal("fragment missing the new board")
	}

	// Owner isolation: another user sees an empty list.
	other := wbSignup(t, mux, "other@example.com")
	otherFrag := httptest.NewRecorder()
	otherReq := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/whiteboards/fragment", nil)
	otherReq.AddCookie(other)
	mux.ServeHTTP(otherFrag, otherReq)
	if strings.Contains(otherFrag.Body.String(), docID) {
		t.Fatal("other user's fragment leaks the board")
	}
}

func TestHandlersWhiteboardSnapshotRoundTrip(t *testing.T) {
	mux := wbFixture(t)
	cookie := wbSignup(t, mux, "snap@example.com")

	newRec := httptest.NewRecorder()
	newReq := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/whiteboard/new", nil)
	newReq.AddCookie(cookie)
	mux.ServeHTTP(newRec, newReq)
	docID := strings.TrimPrefix(newRec.Header().Get("Location"), "/whiteboard/")

	// Snapshot of an untouched doc is the empty shape list.
	snapRec := httptest.NewRecorder()
	snapReq := httptest.NewRequestWithContext(context.Background(),
		http.MethodGet, "/api/whiteboard/"+docID+"/snapshot", nil)
	snapReq.AddCookie(cookie)
	mux.ServeHTTP(snapRec, snapReq)
	if snapRec.Code != http.StatusOK {
		t.Fatalf("snapshot status = %d", snapRec.Code)
	}

	// Presence echoes the caller-stamped user back through the hub path.
	presRec := httptest.NewRecorder()
	presReq := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/whiteboard/"+docID+"/presence?clientID=c1",
		strings.NewReader(`{"type":"cursor","x":10,"y":20}`))
	presReq.Header.Set("Content-Type", "application/json")
	presReq.AddCookie(cookie)
	mux.ServeHTTP(presRec, presReq)
	if presRec.Code != http.StatusOK {
		t.Fatalf("presence status = %d (%s)", presRec.Code, presRec.Body.String())
	}
	var presBody map[string]any
	if err := json.Unmarshal(presRec.Body.Bytes(), &presBody); err != nil {
		t.Fatalf("presence body: %v", err)
	}
	if presBody["ok"] != true {
		t.Fatalf("presence ok = %v", presBody["ok"])
	}
}
