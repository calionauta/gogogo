// SCOPE:layer=feature,removal=feature — Room demo HTTP tests.
package room_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/auth"
	"github.com/calionauta/gogogo/features/room"
	appgoakt "github.com/calionauta/gogogo/internal/goakt"

	_ "github.com/ncruces/go-sqlite3/driver"
)

const (
	roomEmail    = "demo1@demo.app"
	roomPassword = "demo1234456"
	roomEmail2   = "demo2@demo.app"
)

// roomFixture boots PocketBase + a real GoAkt host + the room handler over
// httptest, mirroring router.Init. Auth is real (seeded user + cookie jar +
// password login), so the suite proves the whole path: cookie → grain →
// JSON, including the crash-recovery loop over HTTP.
func roomFixture(t *testing.T) (string, *http.Client, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "room-int-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	cfg := &config.Config{
		Host:          "127.0.0.1",
		Port:          0,
		Dev:           true,
		DataDir:       tmpDir,
		DBPath:        tmpDir + "/app.db",
		EncryptionKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		BuildLabel:    "test",
	}
	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir:       cfg.DataDir,
		DefaultEncryptionEnv: cfg.EncryptionKey,
		DBConnect: func(dbPath string) (*dbx.DB, error) {
			pragmas := "?_pragma=busy_timeout(10000)" +
				"&_pragma=journal_mode(WAL)" +
				"&_pragma=foreign_keys(ON)" +
				"&_pragma=temp_store(MEMORY)"
			return dbx.Open("sqlite3", "file:"+dbPath+pragmas)
		},
	})
	if bErr := app.Bootstrap(); bErr != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Bootstrap: %v", bErr)
	}

	ctx, cancel := context.WithCancel(context.Background())
	host := appgoakt.New()
	if startErr := host.Start(ctx); startErr != nil {
		cancel()
		os.RemoveAll(tmpDir)
		t.Fatalf("goakt start: %v", startErr)
	}

	h := room.New(cfg, host)
	r := router.NewRouter[*core.RequestEvent](
		func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
			e := &core.RequestEvent{App: app}
			e.Response = w
			e.Request = req
			return e, nil
		},
	)
	r.BindFunc(auth.LoadAuthFromCookie)
	h.RegisterRoutesOn(r)
	r.POST("/login", auth.HandlePasswordLogin)

	mux, err := r.BuildMux()
	if err != nil {
		cancel()
		os.RemoveAll(tmpDir)
		t.Fatalf("BuildMux: %v", err)
	}
	seedRoomUser(t, app)

	server := httptest.NewServer(mux)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}
	loginRoom(t, client, server.URL)
	cleanup := func() {
		server.Close()
		cancel()
		_ = app.ClearBootstrap()
		os.RemoveAll(tmpDir)
	}
	return server.URL, client, cleanup
}

func seedRoomUser(t *testing.T, app core.App) {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("users collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.SetEmail(roomEmail)
	rec.SetPassword(roomPassword)
	if err := app.Save(rec); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	rec2 := core.NewRecord(col)
	rec2.SetEmail(roomEmail2)
	rec2.SetPassword(roomPassword)
	if err := app.Save(rec2); err != nil {
		t.Fatalf("seed second user: %v", err)
	}
}

func loginRoom(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()
	loginRoomAs(t, client, baseURL, roomEmail, roomPassword)
}

func loginRoomAs(t *testing.T, client *http.Client, baseURL, email, password string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, baseURL+"/login",
		strings.NewReader(url.Values{"email": {email}, "password": {password}}.Encode()))
	if err != nil {
		t.Fatalf("login req: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	resp.Body.Close()
}

func getJSON(t *testing.T, client *http.Client, url string, out any) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("GET %s req: %v", url, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", url, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
}

func postForm(t *testing.T, client *http.Client, url string) map[string]any {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, nil)
	if err != nil {
		t.Fatalf("POST %s req: %v", url, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s = %d, want 200", url, resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	return out
}

func TestRoomPageRendersRoster(t *testing.T) {
	baseURL, client, cleanup := roomFixture(t)
	defer cleanup()
	postForm(t, client, baseURL+"/room/heartbeat")
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, baseURL+"/room", nil)
	if err != nil {
		t.Fatalf("GET /room req: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /room: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /room = %d, want 200", resp.StatusCode)
	}
}

func TestRoomAcquireCrashRecoverOverHTTP(t *testing.T) {
	baseURL, client, cleanup := roomFixture(t)
	defer cleanup()

	var before struct {
		Presenter  string `json:"presenter"`
		Generation int    `json:"generation"`
	}
	postForm(t, client, baseURL+"/room/heartbeat")
	getJSON(t, client, baseURL+"/room/roster", &before)

	// Second browser, same room: only one presenter wins is covered at
	// grain level; here the HTTP verdict round-trips.
	acquired := postForm(t, client, baseURL+"/room/acquire")
	if _, ok := acquired["presenter"]; !ok {
		t.Fatalf("acquire answer missing presenter: %v", acquired)
	}

	postForm(t, client, baseURL+"/room/crash")
	deadline := time.Now().Add(10 * time.Second)
	for {
		var cur struct {
			Generation int `json:"generation"`
		}
		getJSON(t, client, baseURL+"/room/roster", &cur)
		if cur.Generation > before.Generation {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("generation never advanced past %d after crash", before.Generation)
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Heartbeat rebuilds the restarted roster.
	postForm(t, client, baseURL+"/room/heartbeat")
	var rebuilt struct {
		Members []struct {
			Name string `json:"name"`
		} `json:"members"`
	}
	getJSON(t, client, baseURL+"/room/roster", &rebuilt)
	if len(rebuilt.Members) != 1 || rebuilt.Members[0].Name != roomEmail {
		t.Fatalf("rebuilt roster = %+v, want the demo user only", rebuilt)
	}
}

// TestRoomGuestsRedirectToLogin pins fail-closed auth: no cookie, no
// roster JSON and no mutation — every room route bounces to /login.
func TestRoomGuestsRedirectToLogin(t *testing.T) {
	baseURL, _, cleanup := roomFixture(t)
	defer cleanup()
	guest := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/room"},
		{http.MethodGet, "/room/roster"},
		{http.MethodPost, "/room/heartbeat"},
		{http.MethodPost, "/room/acquire"},
		{http.MethodPost, "/room/crash"},
	} {
		req, err := http.NewRequestWithContext(context.Background(), tc.method, baseURL+tc.path, nil)
		if err != nil {
			t.Fatalf("%s %s req: %v", tc.method, tc.path, err)
		}
		resp, err := guest.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", tc.method, tc.path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusSeeOther {
			t.Errorf("%s %s = %d, want 303 to /login", tc.method, tc.path, resp.StatusCode)
		} else if loc := resp.Header.Get("Location"); !strings.HasSuffix(loc, "/login") {
			t.Errorf("%s %s redirects to %q, want /login", tc.method, tc.path, loc)
		}
	}
}

// TestRoomTwoSessionsOnePresenter races two logged-in browsers for the
// lock over HTTP: exactly one 200-answer may carry a presenter. This is
// the headline property at the transport level, not just the grain.
func TestRoomTwoSessionsOnePresenter(t *testing.T) {
	baseURL, clientA, cleanup := roomFixture(t)
	defer cleanup()
	jarB, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	clientB := &http.Client{Jar: jarB}
	loginRoomAs(t, clientB, baseURL, roomEmail2, roomPassword)

	postForm(t, clientA, baseURL+"/room/heartbeat")
	postForm(t, clientB, baseURL+"/room/heartbeat")

	type verdict struct {
		presenter string
		err       error
	}
	results := make([]verdict, 2)
	var wg sync.WaitGroup
	for i, cl := range []*http.Client{clientA, clientB} {
		wg.Add(1)
		go func(i int, cl *http.Client) {
			defer wg.Done()
			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, baseURL+"/room/acquire", nil)
			if err != nil {
				results[i].err = err
				return
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			resp, err := cl.Do(req)
			if err != nil {
				results[i].err = err
				return
			}
			defer resp.Body.Close()
			var out struct {
				Presenter string `json:"presenter"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
				results[i].err = err
				return
			}
			results[i].presenter = out.Presenter
		}(i, cl)
	}
	wg.Wait()
	for i, r := range results {
		if r.err != nil {
			t.Fatalf("session %d acquire: %v", i, r.err)
		}
	}
	// Both answers carry the CURRENT presenter (mutate returns the fresh
	// roster): they must agree, and it must be one of the two members.
	if results[0].presenter == "" || results[0].presenter != results[1].presenter {
		t.Fatalf("sessions disagree: %q vs %q", results[0].presenter, results[1].presenter)
	}
	if results[0].presenter != roomEmail && results[0].presenter != roomEmail2 {
		t.Fatalf("presenter %q is neither member", results[0].presenter)
	}
}
