// SCOPE:feature — Chi room integration + resilience tests (ported from features/room)
//
// Two shapes, like the framework suite: over-HTTP tests against the real
// GoAkt host (page renders, crash recovery advances the generation, two
// racing sessions agree on one presenter) and unit tests for the
// readRoster retry (transient grain stall tolerated, permanent failure
// bounded). Ported from features/room/web_test.go and
// features/room/handler_resilience_test.go when the legacy handler died.
package chi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	appgoakt "github.com/calionauta/gogogo/internal/goakt"
)

const (
	roomEmailA = "room-a@example.com"
	roomEmailB = "room-b@example.com"
	roomPass   = "Correct-horse-99!"
)

// countingRooms fails the first failTimes roster reads, then succeeds:
// the scripted grain stall the supervised restart tolerates.
type countingRooms struct {
	failTimes int
	calls     int
	roster    appgoakt.Roster
}

func (c *countingRooms) Roster(context.Context, string) (appgoakt.Roster, error) {
	c.calls++
	if c.calls <= c.failTimes {
		return appgoakt.Roster{}, errors.New("transient grain stall")
	}
	return c.roster, nil
}

func (c *countingRooms) Acquire(context.Context, string, string) (appgoakt.AcquireResult, error) {
	return appgoakt.AcquireResult{}, nil
}

func (c *countingRooms) Tell(context.Context, string, any) error { return nil }

// TestReadRosterRetriesTransientFailure pins the supervised-restart
// tolerance: a read that fails once must not surface as a 502 when the
// next attempt succeeds. The grain restart window (and a starved runner)
// is transient by nature; Roster is a pure read, so retrying it is free
// of side effects.
func TestReadRosterRetriesTransientFailure(t *testing.T) {
	rooms := &countingRooms{
		failTimes: 1,
		roster:    appgoakt.Roster{Presenter: "ana@example.com"},
	}
	h := NewHandler(config.Load(), testAuth(t), rooms)
	h.readBudget = 5 * time.Second

	got, err := h.readRoster(context.Background(), "lobby")
	if err != nil {
		t.Fatalf("readRoster after one transient failure: %v", err)
	}
	if rooms.calls != 2 {
		t.Errorf("roster calls = %d, want 2 (one failure + one retry)", rooms.calls)
	}
	if got.Presenter != "ana@example.com" {
		t.Errorf("roster = %+v, want the successful retry's answer", got)
	}
}

// TestReadRosterFailsBoundedOnPermanentError pins the other side: a room
// that is genuinely down (supervisor restart budget exhausted) must still
// fail, with a bounded number of attempts — never retry forever and never
// hang the request.
func TestReadRosterFailsBoundedOnPermanentError(t *testing.T) {
	rooms := &countingRooms{failTimes: 1 << 30}
	h := NewHandler(config.Load(), testAuth(t), rooms)
	h.readBudget = 200 * time.Millisecond

	start := time.Now()
	if _, err := h.readRoster(context.Background(), "lobby"); err == nil {
		t.Fatal("readRoster on a permanently failing grain returned nil error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("readRoster took %v; the read budget must bound it", elapsed)
	}
	if rooms.calls > 3 {
		t.Errorf("roster calls = %d, want at most 3", rooms.calls)
	}
}

// testAuth builds an owned-auth stack on a scratch migrated DB. The
// resilience tests never touch it (readRoster is grain-side only), but
// NewHandler requires a non-nil handle, and building a real one keeps
// the test honest about the wiring.
func testAuth(t *testing.T) *auth.Auth {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/resilience.db")
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
	return a
}

// roomHostFixture boots the REAL GoAkt host (the grains own the room
// state) behind the chi routes, plus the owned auth for session cookies
// — the same composition bootRoomActor wires in production.
func roomHostFixture(t *testing.T) (http.Handler, func(string) *http.Cookie) {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/room-host.db")
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

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	host := appgoakt.New()
	if serr := host.Start(ctx); serr != nil {
		t.Fatalf("goakt start: %v", serr)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = host.Stop(stopCtx)
	})

	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Handler())
	mux.Handle("/", NewHandler(config.Load(), a, host).Routes())
	return mux, func(email string) *http.Cookie { return roomSignup(t, mux, email) }
}

// roomDo drives one request through the mux with a session cookie.
func roomDo(t *testing.T, mux http.Handler, cookie *http.Cookie, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), method, target, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestRoomPageRendersRoster: heartbeat, then the page answers 200 with
// the member visible (the page renders from the grain roster).
func TestRoomPageRendersRoster(t *testing.T) {
	mux, signup := roomHostFixture(t)
	cookie := signup(roomEmailA)

	if rec := roomDo(t, mux, cookie, http.MethodPost, "/room/heartbeat"); rec.Code != http.StatusOK {
		t.Fatalf("heartbeat status = %d, want 200", rec.Code)
	}
	rec := roomDo(t, mux, cookie, http.MethodGet, "/room")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /room = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, roomEmailA) {
		t.Errorf("room page does not render the member %q", roomEmailA)
	}
}

// TestRoomAcquireCrashRecoverOverHTTP: the presenter-lock verdict
// round-trips over HTTP, a crash advances the roster generation, and a
// heartbeat rebuilds the restarted room with the member intact.
func TestRoomAcquireCrashRecoverOverHTTP(t *testing.T) {
	mux, signup := roomHostFixture(t)
	cookie := signup(roomEmailA)

	roomDo(t, mux, cookie, http.MethodPost, "/room/heartbeat")
	var before struct {
		Generation int `json:"generation"`
	}
	if err := json.Unmarshal(roomDo(t, mux, cookie, http.MethodGet, "/room/roster").Body.Bytes(), &before); err != nil {
		t.Fatalf("decode roster: %v", err)
	}

	acquired := roomDo(t, mux, cookie, http.MethodPost, "/room/acquire")
	var verdict struct {
		Presenter string `json:"presenter"`
	}
	if err := json.Unmarshal(acquired.Body.Bytes(), &verdict); err != nil {
		t.Fatalf("decode acquire: %v", err)
	}
	if verdict.Presenter == "" {
		t.Fatalf("acquire answer missing presenter: %s", acquired.Body.String())
	}
	if verdict.Presenter != roomEmailA {
		t.Errorf("presenter = %q, want the acquiring member %q", verdict.Presenter, roomEmailA)
	}

	roomDo(t, mux, cookie, http.MethodPost, "/room/crash")
	deadline := time.Now().Add(10 * time.Second)
	for {
		var cur struct {
			Generation int `json:"generation"`
		}
		body := roomDo(t, mux, cookie, http.MethodGet, "/room/roster").Body.Bytes()
		if err := json.Unmarshal(body, &cur); err != nil {
			t.Fatalf("decode roster after crash: %v", err)
		}
		if cur.Generation > before.Generation {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("generation never advanced past %d after crash", before.Generation)
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Heartbeat rebuilds the restarted roster.
	roomDo(t, mux, cookie, http.MethodPost, "/room/heartbeat")
	var rebuilt struct {
		Members []struct {
			Name string `json:"name"`
		} `json:"members"`
	}
	if err := json.Unmarshal(roomDo(t, mux, cookie, http.MethodGet, "/room/roster").Body.Bytes(), &rebuilt); err != nil {
		t.Fatalf("decode rebuilt roster: %v", err)
	}
	if len(rebuilt.Members) != 1 || rebuilt.Members[0].Name != roomEmailA {
		t.Fatalf("rebuilt roster = %+v, want the member only", rebuilt.Members)
	}
}

// TestRoomTwoSessionsOnePresenter: two sessions racing the presenter lock
// both get the CURRENT presenter back (mutate answers with the fresh
// roster), so the verdicts agree and name one of the two members.
func TestRoomTwoSessionsOnePresenter(t *testing.T) {
	mux, signup := roomHostFixture(t)
	cookieA, cookieB := signup(roomEmailA), signup(roomEmailB)

	roomDo(t, mux, cookieA, http.MethodPost, "/room/heartbeat")
	roomDo(t, mux, cookieB, http.MethodPost, "/room/heartbeat")

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		answers [2]string
	)
	for i, cookie := range []*http.Cookie{cookieA, cookieB} {
		wg.Add(1)
		go func(i int, cookie *http.Cookie) {
			defer wg.Done()
			rec := roomDo(t, mux, cookie, http.MethodPost, "/room/acquire")
			var out struct {
				Presenter string `json:"presenter"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Errorf("session %d acquire decode: %v", i, err)
				return
			}
			mu.Lock()
			answers[i] = out.Presenter
			mu.Unlock()
		}(i, cookie)
	}
	wg.Wait()

	if answers[0] == "" {
		t.Fatalf("no presenter in either verdict: %q / %q", answers[0], answers[1])
	}
	if answers[0] != answers[1] {
		t.Fatalf("sessions disagree: %q vs %q", answers[0], answers[1])
	}
	if answers[0] != roomEmailA && answers[0] != roomEmailB {
		t.Fatalf("presenter %q is neither member", answers[0])
	}
}
