// SCOPE:feature — HTTP notes contract tests (red-proof).
//
// Real Turso file, real auth, real Loro DocStore, driven over HTTP.
// Contracts: paths, statuses, redirects, owner isolation, op apply with
// revision tracking, stale-base 409, typing/caret relay validation.
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

func notesFixture(t *testing.T) http.Handler {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/notes.db")
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

func notesSignup(t *testing.T, mux http.Handler, email string) *http.Cookie {
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

func TestHandlersNotesAuthGate(t *testing.T) {
	mux := notesFixture(t)
	for _, target := range []string{"/notes", "/notes/new", "/api/notes/fragment"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil))
		if rec.Code != http.StatusSeeOther {
			t.Errorf("anon %s: status = %d, want 303", target, rec.Code)
		}
	}
}

func TestHandlersNotesCreateOpAndRev(t *testing.T) {
	mux := notesFixture(t)
	cookie := notesSignup(t, mux, "notes@example.com")

	newRec := httptest.NewRecorder()
	newReq := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/notes/new", nil)
	newReq.AddCookie(cookie)
	mux.ServeHTTP(newRec, newReq)
	if newRec.Code != http.StatusFound {
		t.Fatalf("new status = %d, want 302", newRec.Code)
	}
	docID := strings.TrimPrefix(newRec.Header().Get("Location"), "/notes/")

	// Note page renders empty.
	pageRec := httptest.NewRecorder()
	pageReq := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/notes/"+docID, nil)
	pageReq.AddCookie(cookie)
	mux.ServeHTTP(pageRec, pageReq)
	if pageRec.Code != http.StatusOK {
		t.Fatalf("note page status = %d", pageRec.Code)
	}

	// Apply an op batch at base 0 (insert "hi" at 0).
	opRec := httptest.NewRecorder()
	opReq := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/notes/"+docID+"/op?clientID=c1",
		strings.NewReader(`{"base":0,"ops":[{"t":"ins","i":0,"s":"hi"}]}`))
	opReq.Header.Set("Content-Type", "application/json")
	opReq.AddCookie(cookie)
	mux.ServeHTTP(opRec, opReq)
	if opRec.Code != http.StatusOK {
		t.Fatalf("op status = %d (%s)", opRec.Code, opRec.Body.String())
	}
	var opBody map[string]any
	if err := json.Unmarshal(opRec.Body.Bytes(), &opBody); err != nil {
		t.Fatalf("op body: %v", err)
	}
	if opBody["text"] != "hi" || opBody["rev"] != float64(1) {
		t.Fatalf("op answer = %v, want text=hi rev=1", opBody)
	}

	// Stale base refused with authoritative state.
	staleRec := httptest.NewRecorder()
	staleReq := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/notes/"+docID+"/op?clientID=c1",
		strings.NewReader(`{"base":0,"ops":[{"t":"ins","i":2,"s":"!"}]}`))
	staleReq.Header.Set("Content-Type", "application/json")
	staleReq.AddCookie(cookie)
	mux.ServeHTTP(staleRec, staleReq)
	if staleRec.Code != http.StatusConflict {
		t.Fatalf("stale op status = %d, want 409", staleRec.Code)
	}

	// Owner isolation on the list.
	other := notesSignup(t, mux, "other@example.com")
	otherFrag := httptest.NewRecorder()
	otherReq := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/notes/fragment", nil)
	otherReq.AddCookie(other)
	mux.ServeHTTP(otherFrag, otherReq)
	if strings.Contains(otherFrag.Body.String(), docID) {
		t.Fatal("other user's fragment leaks the note")
	}
}

func TestHandlersNotesTypingCaret(t *testing.T) {
	mux := notesFixture(t)
	cookie := notesSignup(t, mux, "presence@example.com")

	for _, tc := range []struct {
		target string
		body   string
		want   int
	}{
		{"/api/notes/d1/typing", `{"typing":true}`, http.StatusOK},
		{"/api/notes/d1/typing", `nope`, http.StatusBadRequest},
		{"/api/notes/d1/caret", `{"line":3,"pos":7}`, http.StatusOK},
		{"/api/notes/d1/caret", `{"line":0,"pos":7}`, http.StatusBadRequest},
		{"/api/notes/d1/caret", `{"line":3,"pos":-1}`, http.StatusBadRequest},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(),
			http.MethodPost, tc.target+"?clientID=c9", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		mux.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("POST %s %q: status = %d, want %d (body: %s)",
				tc.target, tc.body, rec.Code, tc.want, rec.Body.String())
		}
	}
}
