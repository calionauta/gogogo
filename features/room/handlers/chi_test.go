// SCOPE:feature — HTTP room contract tests (red-proof).
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	appgoakt "github.com/calionauta/gogogo/internal/goakt"
	"github.com/calionauta/gogogo/internal/queue"
)

// fakeRooms is the deterministic grain stand-in: scripted roster,
// recorded tells, injectable failure.
type fakeRooms struct {
	roster appgoakt.Roster
	told   []any
	err    error
}

func (f *fakeRooms) Roster(context.Context, string) (appgoakt.Roster, error) {
	if f.err != nil {
		return appgoakt.Roster{}, f.err
	}
	return f.roster, nil
}

func (f *fakeRooms) Tell(_ context.Context, _ string, msg any) error {
	if f.err != nil {
		return f.err
	}
	f.told = append(f.told, msg)
	return nil
}

func roomFixture(t *testing.T, rooms roomSystem) http.Handler {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/room.db")
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
	h := NewHandler(config.Load(), a, rooms, queue.NewSSEHub())
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Handler())
	mux.Handle("/", h.Routes())
	return mux
}

func roomSignup(t *testing.T, mux http.Handler, email string) *http.Cookie {
	t.Helper()
	body := `{"email":"` + email + `","password":"Correct-horse-99!"}`
	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/auth/signup/credential", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("signup: status = %d", rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func TestHandlersRoomAuthGate(t *testing.T) {
	mux := roomFixture(t, &fakeRooms{})
	for _, tc := range []struct{ method, target string }{
		{http.MethodGet, "/room"},
		{http.MethodGet, "/room/roster"},
		{http.MethodPost, "/room/heartbeat"},
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), tc.method, tc.target, nil))
		if rec.Code != http.StatusSeeOther {
			t.Errorf("%s %s anon: status = %d, want 303", tc.method, tc.target, rec.Code)
		}
	}
}

func TestHandlersRoomFlow(t *testing.T) {
	fake := &fakeRooms{roster: appgoakt.Roster{
		Members: []appgoakt.Member{{Name: "room@example.com"}},
	}}
	mux := roomFixture(t, fake)
	cookie := roomSignup(t, mux, "room@example.com")

	do := func(method, target, body string) *httptest.ResponseRecorder {
		t.Helper()
		var r *http.Request
		switch {
		case body != "":
			r = httptest.NewRequestWithContext(context.Background(),
				http.MethodPost, target, strings.NewReader(body))
		case method == http.MethodPost:
			r = httptest.NewRequestWithContext(context.Background(), http.MethodPost, target, nil)
		default:
			r = httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
		}
		r.AddCookie(cookie)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)
		return rec
	}

	rec := do(http.MethodGet, "/room", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /room status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "room@example.com") {
		t.Fatal("room page missing the member email")
	}

	if rec := do(http.MethodGet, "/room/roster", ""); rec.Code != http.StatusOK {
		t.Fatalf("roster status = %d", rec.Code)
	} else {
		var roster appgoakt.Roster
		if err := json.Unmarshal(rec.Body.Bytes(), &roster); err != nil {
			t.Fatalf("roster decode: %v", err)
		}
		if len(roster.Members) != 1 {
			t.Fatalf("roster members = %d, want 1", len(roster.Members))
		}
	}

	if rec := do(http.MethodPost, "/room/heartbeat", ""); rec.Code != http.StatusOK {
		t.Fatalf("heartbeat status = %d", rec.Code)
	}
	if len(fake.told) != 1 {
		t.Fatalf("told = %d messages, want 1 heartbeat", len(fake.told))
	}
	if _, ok := fake.told[0].(appgoakt.RoomHeartbeat); !ok {
		t.Fatalf("told[0] = %T, want RoomHeartbeat", fake.told[0])
	}

	fake.err = errors.New("grain down")
	if rec := do(http.MethodGet, "/room/roster", ""); rec.Code != http.StatusBadGateway {
		t.Fatalf("roster on grain failure status = %d, want 502", rec.Code)
	}
}

// TestNilHandlerRoutesIsNil pins the disabled-engine contract: BootChi calls
// Routes() on the nil handler it gets when GoAkt is off, and MountFeature must
// mount nothing (the route then answers 404, never a panic).
func TestNilHandlerRoutesIsNil(t *testing.T) {
	var h *Handler
	if r := h.Routes(); r != nil {
		t.Fatalf("nil Handler Routes() = %v, want nil", r)
	}
}
