// SCOPE:core — Flat-stack boot tests (red-proof for the serve seam).
package server

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/config"
	chatchi "github.com/calionauta/gogogo/features/chat/chi"
	chatstore "github.com/calionauta/gogogo/features/chat/store"
	configchi "github.com/calionauta/gogogo/features/config/chi"
	creditschi "github.com/calionauta/gogogo/features/credits/chi"
	genuichi "github.com/calionauta/gogogo/features/genui/chi"
	landingchi "github.com/calionauta/gogogo/features/landing/chi"
	notesachi "github.com/calionauta/gogogo/features/notes/chi"
	roomchi "github.com/calionauta/gogogo/features/room/chi"
	todochi "github.com/calionauta/gogogo/features/todo/chi"
	wbchi "github.com/calionauta/gogogo/features/whiteboard/chi"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/collab"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	appgoakt "github.com/calionauta/gogogo/internal/goakt"
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
	rows, rerr := sqlDB.QueryContext(context.Background(), `SELECT version FROM schema_migrations ORDER BY version`)
	if rerr != nil {
		t.Fatalf("read migrations: %v (seam did not migrate)", rerr)
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		if serr := rows.Scan(&v); serr != nil {
			t.Fatalf("scan: %v", serr)
		}
		versions = append(versions, v)
	}
	if rerr := rows.Err(); rerr != nil {
		t.Fatalf("rows: %v", rerr)
	}
	want, verr := db.Versions()
	if verr != nil {
		t.Fatalf("expected versions: %v", verr)
	}
	if fmt.Sprintf("%v", versions) != fmt.Sprintf("%v", want) {
		t.Fatalf("applied ledger = %v, want embedded %v", versions, want)
	}
}

// TestServeChiOnListener is the automated preflight: the production boot
// composition served on a real :0 port, driven over real HTTP (signup,
// login form, authed todo create). Ephemeral port read back from the
// listener — never fixed, never flaky under parallel packages.
func TestServeChiOnListener(t *testing.T) {
	cfg := config.Load()
	cfg.Dev = true
	cfg.Turso.DBPath = t.TempDir() + "/serve.db"

	bundle, err := BootChi(cfg)
	if err != nil {
		t.Fatalf("BootChi: %v", err)
	}
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	stop := serveChiOnListener(ln, srvHandler(bundle.mux), bundle)
	defer stop()
	base := "http://" + ln.Addr().String()

	jar := &cookieJar{}
	client := &http.Client{Jar: jar, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	post := func(path, body, contentType string) *http.Response {
		t.Helper()
		req, rerr := http.NewRequestWithContext(context.Background(),
			http.MethodPost, base+path, strings.NewReader(body))
		if rerr != nil {
			t.Fatalf("request %s: %v", path, rerr)
		}
		req.Header.Set("Content-Type", contentType)
		resp, derr := client.Do(req)
		if derr != nil {
			t.Fatalf("POST %s: %v", path, derr)
		}
		defer resp.Body.Close()
		return resp
	}

	signupBody := `{"email":"serve@example.com","password":"Correct-horse-99!"}`
	if resp := post("/api/auth/signup/credential",
		signupBody, "application/json"); resp.StatusCode != http.StatusOK {
		t.Fatalf("signup status = %d", resp.StatusCode)
	}
	if resp := post("/login",
		"email=serve@example.com&password=Correct-horse-99!&next=/todo",
		"application/x-www-form-urlencoded"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("form login status = %d", resp.StatusCode)
	}
	if resp := post("/api/todos?filter=all", "title=Served+item",
		"application/x-www-form-urlencoded"); resp.StatusCode != http.StatusOK {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/todo", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /todo: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /todo status = %d", resp.StatusCode)
	}
}

// cookieJar is the smallest jar that keeps the test honest: session
// cookies round-trip, nothing else is remembered.
type cookieJar struct {
	cookies []*http.Cookie
}

func (j *cookieJar) SetCookies(_ *url.URL, cs []*http.Cookie) {
	j.cookies = append(j.cookies, cs...)
}

func (j *cookieJar) Cookies(_ *url.URL) []*http.Cookie {
	return j.cookies
}

// fakeRooms is the deterministic grain stand-in for mux tests.
type fakeRooms struct{}

func (fakeRooms) Roster(_ context.Context, _ string) (appgoakt.Roster, error) {
	return appgoakt.Roster{}, nil
}

func (fakeRooms) Acquire(_ context.Context, _, _ string) (appgoakt.AcquireResult, error) {
	return appgoakt.AcquireResult{}, nil
}

func (fakeRooms) Tell(_ context.Context, _ string, _ any) error { return nil }

// stubResponder answers model calls with fixed output.
type stubResponder struct{ raw string }

func (s stubResponder) Respond(context.Context, string) (string, error) { return s.raw, nil }

// stubGenuiResponder answers Ask calls with an empty catalog.
type stubGenuiResponder struct{}

func (stubGenuiResponder) Respond(context.Context, string) (string, error) {
	return `{"components":[]}`, nil
}

// chiQForTest opens an isolated queue (no workers needed for routing).
func chiQForTest(t *testing.T) *queue.Queue {
	t.Helper()
	q, err := queue.NewAt(t.TempDir() + "/muxqueue")
	if err != nil {
		t.Fatalf("mux queue: %v", err)
	}
	t.Cleanup(q.Close)
	return q
}

// TestBuildChiMuxRoomDisabledPinsRouteAbsence is the port of the router
// suite's RegisterRoomStackDisabledRegistersNothing: when the actor system
// is disabled the handler is nil and the mux must not carry /room at all —
// a dead route answering 502 is worse than a 404 that says "not here".
func TestBuildChiMuxRoomDisabledPinsRouteAbsence(t *testing.T) {
	cfg := config.Load()
	sqlDB, err := db.OpenAt(t.TempDir() + "/noroom.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("db migrate: %v", err)
	}
	q := dbgen.New(sqlDB)
	a, aerr := auth.New(q, false)
	if aerr != nil {
		t.Fatalf("auth new: %v", aerr)
	}
	persister, perr := collab.NewTursoPersister(q)
	if perr != nil {
		t.Fatalf("persister: %v", perr)
	}
	docs := collab.NewDocStore()
	hub := queue.NewSSEHub()
	mux := buildChiMux(a, todochi.NewHandler(q, a, hub, "daisyui"),
		landingchi.NewHandler(cfg, a), configchi.NewHandler(cfg, a),
		wbchi.NewHandler(q, a, hub, persister, docs, cfg),
		notesachi.NewHandler(q, a, hub, persister, docs, cfg),
		nil, // roomHandler: engine disabled
		chatchi.NewHandler(cfg, a, chiQForTest(t), queue.NewSSEHub(), chatstore.NewMemStore(), stubResponder{raw: "{}"}),
		genuichi.NewHandler(cfg, a, chiQForTest(t), queue.NewSSEHub(), stubGenuiResponder{}),
		creditschi.NewHandler(a, nil))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/room", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /room with the engine disabled: status = %d, want 404", rec.Code)
	}
}

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
	cfg := config.Load()
	persister, perr := collab.NewTursoPersister(dbgen.New(sqlDB))
	if perr != nil {
		t.Fatalf("persister: %v", perr)
	}
	docs := collab.NewDocStore()

	hub := queue.NewSSEHub()
	mux := buildChiMux(a, todoH,
		landingchi.NewHandler(cfg, a), configchi.NewHandler(cfg, a),
		wbchi.NewHandler(dbgen.New(sqlDB), a, hub, persister, docs, cfg),
		notesachi.NewHandler(dbgen.New(sqlDB), a, hub, persister, docs, cfg),
		roomchi.NewHandler(cfg, a, fakeRooms{}),
		chatchi.NewHandler(cfg, a, chiQForTest(t), queue.NewSSEHub(), chatstore.NewMemStore(), stubResponder{raw: "{}"}),
		genuichi.NewHandler(cfg, a, chiQForTest(t), queue.NewSSEHub(), stubGenuiResponder{}),
		creditschi.NewHandler(a, nil))
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

	// Page routes ride the same mux (this composition broke once before).
	loginRec := httptest.NewRecorder()
	mux.ServeHTTP(loginRec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/login", nil))
	if loginRec.Code != http.StatusOK {
		t.Fatalf("mux GET /login status = %d, want 200", loginRec.Code)
	}
	anonRec := httptest.NewRecorder()
	mux.ServeHTTP(anonRec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/todo", nil))
	if anonRec.Code != http.StatusSeeOther {
		t.Fatalf("mux anon /todo status = %d, want 303", anonRec.Code)
	}
	landingRec := httptest.NewRecorder()
	mux.ServeHTTP(landingRec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	if landingRec.Code != http.StatusOK {
		t.Fatalf("mux GET / status = %d, want 200", landingRec.Code)
	}
	configRec := httptest.NewRecorder()
	mux.ServeHTTP(configRec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/config", nil))
	if configRec.Code != http.StatusSeeOther {
		t.Fatalf("mux anon /config status = %d, want 303", configRec.Code)
	}
	for _, target := range []string{"/room", "/chat", "/genui"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil))
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("mux anon %s status = %d, want 303", target, rec.Code)
		}
	}
	creditsRec := httptest.NewRecorder()
	mux.ServeHTTP(creditsRec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/credits", nil))
	if creditsRec.Code != http.StatusUnauthorized {
		t.Fatalf("mux anon /api/credits status = %d, want 401 JSON", creditsRec.Code)
	}
}
