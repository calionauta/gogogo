// The page fixtures: a real database, the real auth stack, driven over HTTP.
//
// Mirrors the todo handler fixture's philosophy — nothing is faked except the
// things a page test has no business exercising — but builds only what the
// sign-in surfaces need.
package authpages

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// guardedProbe is a page that requires a session, mounted by the fixture only.
//
// The tests need to prove "a guest is bounced, a session gets through" without
// depending on a slice: /todo used to play this role, which made the auth suite
// red on any product that dropped the demo.
const guardedProbe = "/probe"

// pageFixture is one booted set of sign-in pages.
type pageFixture struct {
	t     *testing.T
	r     http.Handler
	h     *Handler
	q     *dbgen.Queries
	sqlDB *sql.DB
	auth  *auth.Auth
}

func newPageFixture(t *testing.T) *pageFixture {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/authpages.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("migrate: %v", merr)
	}
	a, err := auth.New(dbgen.New(sqlDB), false)
	if err != nil {
		t.Fatalf("auth new: %v", err)
	}
	h := New(a, dbgen.New(sqlDB), false, true)

	probe := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := a.Session(r); err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Routes())
	mux.Handle(guardedProbe, probe)
	mux.Handle("/", h.Routes())
	return &pageFixture{t: t, r: mux, h: h, q: dbgen.New(sqlDB), sqlDB: sqlDB, auth: a}
}

// signup creates a user over HTTP. It goes through the API half on purpose:
// that is how a real client arrives at a signed-in state, and it keeps the page
// tests from re-implementing signup.
func (f *pageFixture) signup(email string) {
	f.t.Helper()
	body := `{"email":` + strconv.Quote(email) + `,"password":"Correct-horse-99!"}`
	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/auth/signup/credential", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		f.t.Fatalf("signup %s: status = %d (body: %s)", email, rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName {
			return
		}
	}
	f.t.Fatalf("signup %s: no %q cookie", email, auth.CookieName)
}

// do drives one request. `form` is an urlencoded body, `cookie` the session (if
// any), `extra` any additional cookie the case needs.
func (f *pageFixture) do(
	method, target, form string, cookie *http.Cookie,
) *httptest.ResponseRecorder {
	f.t.Helper()
	var reader io.Reader
	if form != "" {
		reader = strings.NewReader(form)
	}
	req := httptest.NewRequestWithContext(context.Background(), method, target, reader)
	if form != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	return rec
}
