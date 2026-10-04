package todo_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/auth"
	"github.com/calionauta/gogogo/features/store/crdtstore"
	"github.com/calionauta/gogogo/features/todo/handlers"
	"github.com/calionauta/gogogo/internal/nats"
	"github.com/calionauta/gogogo/internal/queue"
)

// TestRepro_CRDT_CreateStuckLoading reproduces the live bug from issue
// CAL-2: in ENTITY_STORE=crdt mode, clicking "add task" leaves the UI
// stuck in loading/disabled and the /api/todos endpoint returns an
// error in the network tab.
//
// Root cause hypothesis: handleCreate builds a todo.Todo with an EMPTY
// ID. PBStore tolerates this (PocketBase auto-generates the record id),
// but CRDTStore.Create requires a non-empty client-generated id
// (it keys the Loro map by it and returns
// "crdtstore: empty todo ID (client must generate UUID)"). The handler
// never forwards the client's idem_key into item.ID, so every create
// 500s in crdt mode — exactly the symptom the user reports.
func TestRepro_CRDT_CreateStuckLoading(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "todo-crdt-int-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	key := make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		t.Fatalf("rand key: %v", err)
	}
	cfg := &config.Config{
		Host:          "127.0.0.1",
		Port:          0,
		Dev:           true,
		DataDir:       tmpDir,
		DBPath:        tmpDir + "/app.db",
		EncryptionKey: hex.EncodeToString(key),
	}

	app := newPocketBaseApp(cfg)
	if bootErr := app.Bootstrap(); bootErr != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Bootstrap: %v", bootErr)
	}

	// Production schema for todos: title, completed, created, updated,
	// owner (text id), and idem_key (text) with the (idem_key, owner)
	// unique index CRDTStore relies on for idempotent upserts.
	if err = createTodosCollectionCRDT(app); err != nil {
		mustReset(t, app)
		os.RemoveAll(tmpDir)
		t.Fatalf("create todos collection (crdt): %v", err)
	}

	q, err := queue.New(cfg)
	if err != nil {
		mustReset(t, app)
		os.RemoveAll(tmpDir)
		t.Fatalf("queue.New: %v", err)
	}

	cs := crdtstore.New(app)
	if err = cs.EnsureSchema(); err != nil {
		t.Fatalf("crdt EnsureSchema: %v", err)
	}

	h := handlers.New(app, q, cfg)
	h.SetStore(cs)
	h.SetBroadcaster(nats.NewInMemoryBroadcaster(q.Hub()))
	h.RegisterHandlers(q.Registry())

	if err = seedDemoUserInline(app); err != nil {
		t.Fatalf("seedDemoUserInline: %v", err)
	}

	r := router.NewRouter[*core.RequestEvent](newRequestEventFactory(app))
	auth.CookieSecure = false
	r.BindFunc(auth.LoadAuthFromCookie)
	h.RegisterRoutesOn(r)
	r.GET("/login", auth.RedirectIfAuthed).BindFunc(auth.HandleLoginGetForTest)
	r.POST("/login", auth.HandlePasswordLogin)
	r.POST("/logout", auth.HandleLogout)

	mux, err := r.BuildMux()
	if err != nil {
		q.Close()
		mustReset(t, app)
		os.RemoveAll(tmpDir)
		t.Fatalf("BuildMux: %v", err)
	}
	server := httptest.NewServer(mux)
	defer server.Close()
	defer q.Close()

	ctx := context.Background()
	client := loginClient(ctx, t, server.URL)

	uuid := "11111111-1111-1111-1111-111111111111"

	// 1) CREATE — mirrors the live frontend: POST /api/todos with a
	//    client-generated idem_key. In the buggy code this 500s (stuck
	//    loading); after the fix it must return 200.
	resp, err := postFormAuth(ctx, client, server.URL+"/api/todos", url.Values{
		titleField: {buyMilk},
		"idem_key": {uuid},
	})
	if err != nil {
		t.Fatalf("POST /api/todos: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body := readBody(t, resp)
		t.Fatalf("CREATE expected 200, got %d (body=%s)", resp.StatusCode, body)
	}

	records, rerr := app.FindRecordsByFilter("todos", "", "", 0, 0)
	if rerr != nil {
		t.Fatalf("list after create: %v", rerr)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 todo record after create, got %d", len(records))
	}
	// In crdt mode the public todo id is the Loro key (the client
	// idem_key we sent), NOT the PocketBase row id — this is what the
	// frontend renders into the toggle/delete URLs after create.
	id := records[0].GetString("idem_key")
	if id != uuid {
		t.Fatalf("expected todo idem_key=%s, got %s", uuid, id)
	}

	// 2) TOGGLE — must also work in crdt mode (keys by the todo id).
	tresp, err := postFormAuth(ctx, client, server.URL+"/api/todos/"+id+"/toggle", nil)
	if err != nil {
		t.Fatalf("POST toggle: %v", err)
	}
	if tresp.StatusCode != http.StatusOK {
		body := readBody(t, tresp)
		t.Fatalf("TOGGLE expected 200, got %d (body=%s)", tresp.StatusCode, body)
	}

	// 3) CLEAR COMPLETED ("remove all") — must work in crdt mode.
	crep, err := postFormAuth(ctx, client, server.URL+"/api/todos/completed/delete", nil)
	if err != nil {
		t.Fatalf("POST clear completed: %v", err)
	}
	if crep.StatusCode != http.StatusOK {
		body := readBody(t, crep)
		t.Fatalf("CLEAR COMPLETED expected 200, got %d (body=%s)", crep.StatusCode, body)
	}
}

// createTodosCollectionCRDT builds the todos schema with the idem_key
// field + (idem_key, owner) unique index, matching the production seed
// that CRDTStore depends on.
func createTodosCollectionCRDT(app core.App) error {
	col := core.NewBaseCollection("todos")
	col.Fields.Add(
		&core.TextField{Name: titleField, Required: true},
		&core.BoolField{Name: "completed"},
		&core.DateField{Name: "created"},
		&core.DateField{Name: "updated"},
		&core.TextField{Name: "owner"},
		&core.TextField{Name: "idem_key", Max: 64},
	)
	col.AddIndex("idx_todos_idem_owner", true, "idem_key", "owner")
	return app.Save(col)
}

// postFormAuth sends a form POST using the supplied (cookie-carrying)
// client and returns the response (body left open for the caller).
func postFormAuth(ctx context.Context, client *http.Client, rawURL string, vals url.Values) (*http.Response, error) {
	var body *strings.Reader
	if vals != nil {
		body = strings.NewReader(vals.Encode())
	} else {
		body = strings.NewReader("")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return client.Do(req)
}
