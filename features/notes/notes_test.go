// SCOPE:layer=feature,removal=feature — Notes collab tests (red-first:
// this file landed before the implementation and failed to compile).
package notes_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/auth"
	"github.com/calionauta/gogogo/features/notes"
	"github.com/calionauta/gogogo/internal/collab"
	"github.com/calionauta/gogogo/internal/queue"

	_ "github.com/ncruces/go-sqlite3/driver"
)

const (
	notesEmail    = "demo@demo.app"
	notesPassword = "demo1234456"
)

// notesFixture boots PocketBase + queue + the notes handler over httptest,
// mirroring router.Init. SSE-only (nil NATS): same-instance live updates,
// server-state convergence only. In-memory persister; production uses the
// PocketBase "notes" collection via the same Persister interface.
func notesFixture(t *testing.T) (string, *collab.MemoryPersister, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "notes-int-*")
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

	q, err := queue.New(cfg)
	if err != nil {
		mustReset(t, app)
		os.RemoveAll(tmpDir)
		t.Fatalf("queue.New: %v", err)
	}

	persister := collab.NewMemoryPersister()
	docs := collab.NewDocStore()
	h := notes.New(app, q.Hub(), persister, docs, nil, cfg)

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
		q.Close()
		mustReset(t, app)
		os.RemoveAll(tmpDir)
		t.Fatalf("BuildMux: %v", err)
	}
	seedNotesUser(t, app)

	server := httptest.NewServer(mux)
	cleanup := func() {
		server.Close()
		q.Close()
		mustReset(t, app)
		os.RemoveAll(tmpDir)
	}
	return server.URL, persister, cleanup
}

func seedNotesUser(t *testing.T, app core.App) {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("users collection: %v", err)
	}
	if existing, fErr := app.FindAuthRecordByEmail(col.Name, notesEmail); fErr == nil && existing != nil {
		existing.SetPassword(notesPassword)
		if sErr := app.Save(existing); sErr != nil {
			t.Fatalf("save existing user: %v", err)
		}
		return
	}
	rec := core.NewRecord(col)
	rec.SetEmail(notesEmail)
	rec.SetPassword(notesPassword)
	if err := app.Save(rec); err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

func notesLogin(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, baseURL+"/login",
		strings.NewReader(url.Values{"email": {notesEmail}, "password": {notesPassword}}.Encode()))
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

func mustReset(t *testing.T, app core.App) {
	t.Helper()
	if err := app.ClearBootstrap(); err != nil {
		t.Logf("ClearBootstrap: %v", err)
	}
}

func notesAuthedClient(t *testing.T, baseURL string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}
	notesLogin(t, client, baseURL)
	return client
}

// postOp sends one op batch as clientID and returns the resolved text.
func postOp(t *testing.T, client *http.Client, baseURL, docID, clientID, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		baseURL+"/api/notes/"+docID+"/op?clientID="+clientID,
		strings.NewReader(body))
	if err != nil {
		t.Fatalf("op req: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("op post: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if dErr := json.NewDecoder(resp.Body).Decode(&out); dErr != nil {
		out = map[string]any{} // non-JSON error bodies (400 plain text) assert on status, not payload
	}
	return resp.StatusCode, out
}

// TestNotesOpRoundTrip pins the core contract: insert ops resolve to text,
// later ops append, and the page renders the resolved state.
func TestNotesOpRoundTrip(t *testing.T) {
	baseURL, _, cleanup := notesFixture(t)
	defer cleanup()
	client := notesAuthedClient(t, baseURL)
	docID := "note-roundtrip"

	code, out := postOp(t, client, baseURL, docID, "cliA", `{"ops":[{"t":"ins","i":0,"s":"hello"}]}`)
	if code != http.StatusOK {
		t.Fatalf("op status = %d: %v", code, out)
	}
	if out["text"] != "hello" {
		t.Fatalf("text = %v, want hello", out["text"])
	}

	code, out = postOp(t, client, baseURL, docID, "cliA", `{"ops":[{"t":"ins","i":5,"s":" world"}]}`)
	if code != http.StatusOK || out["text"] != "hello world" {
		t.Fatalf("append: status = %d, out = %v", code, out)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, baseURL+"/notes/"+docID, nil)
	if err != nil {
		t.Fatalf("page req: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("page get: %v", err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("page read: %v", err)
	}
	if !strings.Contains(buf.String(), "hello world") {
		t.Fatalf("page does not contain resolved text")
	}
}

// TestNotesConcurrentInsertsMerge is the CRDT proof: two clients typing at
// the same position must both survive — no last-write-wins data loss.
func TestNotesConcurrentInsertsMerge(t *testing.T) {
	baseURL, _, cleanup := notesFixture(t)
	defer cleanup()
	client := notesAuthedClient(t, baseURL)
	docID := "note-merge"

	postOp(t, client, baseURL, docID, "cliA", `{"ops":[{"t":"ins","i":0,"s":"hello"}]}`)
	_, out := postOp(t, client, baseURL, docID, "cliB", `{"ops":[{"t":"ins","i":0,"s":"XYZ "}]}`)
	text, _ := out["text"].(string)
	if !strings.Contains(text, "hello") || !strings.Contains(text, "XYZ") {
		t.Fatalf("merge lost an edit: %q", text)
	}
}

// TestNotesDelete pins delete semantics with clamping.
func TestNotesDelete(t *testing.T) {
	baseURL, _, cleanup := notesFixture(t)
	defer cleanup()
	client := notesAuthedClient(t, baseURL)
	docID := "note-delete"

	postOp(t, client, baseURL, docID, "cliA", `{"ops":[{"t":"ins","i":0,"s":"hello world"}]}`)
	_, out := postOp(t, client, baseURL, docID, "cliA", `{"ops":[{"t":"del","i":5,"n":99}]}`)
	if out["text"] != "hello" {
		t.Fatalf("delete clamped: text = %v", out["text"])
	}
}

// TestNotesOpValidation pins rejection of malformed ops.
func TestNotesOpValidation(t *testing.T) {
	baseURL, _, cleanup := notesFixture(t)
	defer cleanup()
	client := notesAuthedClient(t, baseURL)
	docID := "note-bad"

	if code, _ := postOp(t, client, baseURL, docID, "cliA",
		`{"ops":[{"t":"explode","i":0}]}`); code != http.StatusBadRequest {
		t.Fatalf("unknown op type: status = %d, want 400", code)
	}
	if code, _ := postOp(t, client, baseURL, docID, "cliA", `not json`); code != http.StatusBadRequest {
		t.Fatalf("bad json: status = %d, want 400", code)
	}
}

// TestNotesPersistsSnapshot pins that the resolved snapshot survives the
// op path through the Persister interface (production: "notes" collection).
func TestNotesPersistsSnapshot(t *testing.T) {
	baseURL, persister, cleanup := notesFixture(t)
	defer cleanup()
	client := notesAuthedClient(t, baseURL)
	docID := "note-snap"

	postOp(t, client, baseURL, docID, "cliA", `{"ops":[{"t":"ins","i":0,"s":"persist me"}]}`)
	snap, ok := persister.LoadSnapshot(docID)
	if !ok || len(snap) == 0 {
		t.Fatalf("no snapshot persisted for %q", docID)
	}
}
