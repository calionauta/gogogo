// The pomodoro recipe's contract tests.
//
// They ship with the recipe on purpose: after `recipe install` these files land
// in the consumer's tree as ordinary `_test.go` files, so `go test ./...` runs
// them with every other package. A recipe that arrives with no tests is a
// recipe whose behavior nobody pins — `recipe check` now reports that as an
// error, and this file is the shape it points at.
//
// What each test is worth, in one line:
//
//   - the declaration is a claim, so it is compared to the routes the provider
//     actually mounts (both directions: no undeclared route, no declared route
//     that nothing serves);
//   - the write is asserted at the wire (303 + the row on the page afterwards),
//     because a 200 on the POST says nothing about what the list renders;
//   - ownership is asserted from a second account, because "the list is scoped
//     in SQL" is the sentence that turns into a data leak when it stops being
//     true.
package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/recipe"
)

const testPassword = "Correct-horse-99!"

// pomodoroStack boots the recipe against a real (temporary) store: the core
// ledger first, then the recipe's own migration set through the same path the
// composition root uses. A test that skipped the migration would pass against a
// table that production does not have.
func pomodoroStack(t *testing.T) (http.Handler, *auth.Auth, *Handler) {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/pomodoro.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	a, aerr := auth.New(dbgen.New(sqlDB), false)
	if aerr != nil {
		t.Fatalf("auth new: %v", aerr)
	}
	h := NewHandler(a, dbgen.New(sqlDB), config.Load())
	var reg recipe.Registry
	reg.Add(h.Recipe())
	if merr := dbschema.MigrateWith(sqlDB, reg.MigrationSets()...); merr != nil {
		t.Fatalf("db migrate: %v", merr)
	}
	mux := http.NewServeMux()
	mux.Handle("/", h.Routes())
	return mux, a, h
}

// signupCookie registers the account and returns the session cookie, over the
// real auth path (Signup opens the session), so the handler's own session check
// is exercised rather than bypassed.
func signupCookie(t *testing.T, a *auth.Auth, email string) *http.Cookie {
	t.Helper()
	_, token, err := a.Signup(context.Background(), email, testPassword, "")
	if err != nil {
		t.Fatalf("signup %s: %v", email, err)
	}
	if token == "" {
		t.Fatal("signup returned no session token")
	}
	return &http.Cookie{Name: auth.CookieName, Value: token}
}

func do(t *testing.T, h http.Handler, req *http.Request, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func get(t *testing.T, h http.Handler, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	return do(t, h, httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil), cookie)
}

// TestPomodoroDeclarationMatchesItsRoutes pins the claim the installer and
// `recipe check` trust: the declared surface IS the mounted surface.
func TestPomodoroDeclarationMatchesItsRoutes(t *testing.T) {
	_, _, handler := pomodoroStack(t)

	decl := handler.Recipe().Declaration()
	mounted := map[string]bool{}
	walkErr := chi.Walk(handler.Routes(), func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		mounted[method+" "+pattern] = true
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk routes: %v", walkErr)
	}
	declared := map[string]bool{}
	for _, rt := range decl.Routes {
		declared[rt.Method+" "+rt.Pattern] = true
	}
	for key := range declared {
		if !mounted[key] {
			t.Errorf("declared %s but the provider mounts no such route (install and `recipe check` would fail)", key)
		}
	}
	for key := range mounted {
		if !declared[key] {
			t.Errorf("mounted %s without declaring it (no policy row, no headless verb, no uninstall story)", key)
		}
	}
}

// TestPomodoroWriteArrivesOnThePage pins the wire symptom: the POST redirects
// back to the page, and the page then lists what was written. A handler that
// returns 500, or one that writes and renders nothing, both fail here.
func TestPomodoroWriteArrivesOnThePage(t *testing.T) {
	h, a, _ := pomodoroStack(t)
	cookie := signupCookie(t, a, "alice@example.com")

	form := strings.NewReader("label=write+the+report&minutes=30")
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/pomodoro/sessions", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := do(t, h, req, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST status = %d, want 303 (see other back to the page)", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/pomodoro" {
		t.Fatalf("POST redirect = %q, want /pomodoro", loc)
	}

	page := get(t, h, "/pomodoro", cookie)
	if page.Code != http.StatusOK {
		t.Fatalf("GET /pomodoro status = %d, want 200", page.Code)
	}
	body := page.Body.String()
	if !strings.Contains(body, "write the report") {
		t.Error("page does not list the session that was just written")
	}
	if !strings.Contains(body, "30 min") {
		t.Error("page does not render the stored duration (30 min)")
	}
}

// TestPomodoroListIsOwnerScoped is the test that would have caught a missing
// `WHERE owner = ?`: a second account must not see the first account's rows.
func TestPomodoroListIsOwnerScoped(t *testing.T) {
	h, a, _ := pomodoroStack(t)
	alice := signupCookie(t, a, "alice@example.com")
	bob := signupCookie(t, a, "bob@example.com")

	form := strings.NewReader("label=alice+private&minutes=25")
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/pomodoro/sessions", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if rec := do(t, h, req, alice); rec.Code != http.StatusSeeOther {
		t.Fatalf("alice POST status = %d, want 303", rec.Code)
	}

	if body := get(t, h, "/pomodoro", alice).Body.String(); !strings.Contains(body, "alice private") {
		t.Fatal("alice cannot see her own session")
	}
	if body := get(t, h, "/pomodoro", bob).Body.String(); strings.Contains(body, "alice private") {
		t.Fatal("bob sees alice's session: the list is not scoped to the owner")
	}
}

// TestPomodoroRejectsABlankLabel pins the one validation rule the write owns,
// and that a rejected write stores nothing.
func TestPomodoroRejectsABlankLabel(t *testing.T) {
	h, a, _ := pomodoroStack(t)
	cookie := signupCookie(t, a, "alice@example.com")

	form := strings.NewReader("label=+++&minutes=25")
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/pomodoro/sessions", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := do(t, h, req, cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("blank label status = %d, want 400", rec.Code)
	}
	if body := get(t, h, "/pomodoro", cookie).Body.String(); !strings.Contains(body, "No sessions yet") {
		t.Error("a rejected write still rendered a row")
	}
}

// TestPomodoroSendsAnonymousViewersToLogin pins the session check the handler
// does itself: RBAC may be off in development, so a page that trusts the policy
// reads rows for nobody.
func TestPomodoroSendsAnonymousViewersToLogin(t *testing.T) {
	h, _, _ := pomodoroStack(t)
	rec := get(t, h, "/pomodoro", nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("anonymous GET status = %d, want 303 to /login", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "/login?next=") || !strings.Contains(loc, "auth=required") {
		t.Fatalf("anonymous redirect = %q, want /login?next=...&auth=required", loc)
	}
}
