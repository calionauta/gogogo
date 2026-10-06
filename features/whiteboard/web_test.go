package whiteboard_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"golang.org/x/crypto/bcrypt"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/auth"
	"github.com/calionauta/gogogo/features/whiteboard"
	"github.com/calionauta/gogogo/internal/collab"
	"github.com/calionauta/gogogo/internal/queue"

	_ "github.com/ncruces/go-sqlite3/driver"
)

const (
	wbEmail    = "demo@demo.app"
	wbPassword = "demo1234456"
)

// webFixture boots PocketBase + a queue (SSE hub) + the whiteboard handler
// over httptest, mirroring router.Init. Uses an in-memory persister so the
// test asserts persistence without a real whiteboards collection, then the
// same assertions hold for the PocketBase persister in production.
func webFixture(t *testing.T) (string, *collab.MemoryPersister, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "wb-int-*")
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

	// Drop the users password field's bcrypt cost to the minimum. The seeded
	// password is hashed at bcrypt.DefaultCost (10), which under -race costs
	// ~1.0s to VERIFY — and this fixture logs in once or twice per test, so
	// the default cost alone was ~12s of this package's runtime. Cost is a
	// per-collection field (core.PasswordField.Cost), not a global, so this
	// changes nothing outside the test app; the hash is still a real bcrypt
	// hash, so password validation is genuinely exercised. Same fix, same
	// reasoning as features/todo/fixture_test.go.
	if costErr := lowerPasswordCost(app); costErr != nil {
		mustReset(t, app)
		os.RemoveAll(tmpDir)
		t.Fatalf("lower password cost: %v", costErr)
	}

	q, err := queue.New(cfg)
	if err != nil {
		mustReset(t, app)
		os.RemoveAll(tmpDir)
		t.Fatalf("queue.New: %v", err)
	}

	persister := collab.NewMemoryPersister()
	docs := collab.NewDocStore()
	h := whiteboard.New(app, q.Hub(), persister, docs, nil, cfg)

	r := router.NewRouter[*core.RequestEvent](
		func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
			// Assign via promoted fields: the literal form needs the
			// Event: name next to App:, which modernize flags but Go requires.
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
	// Seed a demo user so login works.
	seedUser(t, app)

	server := httptest.NewServer(mux)
	cleanup := func() {
		server.Close()
		q.Close()
		mustReset(t, app)
		os.RemoveAll(tmpDir)
	}
	return server.URL, persister, cleanup
}

func seedUser(t *testing.T, app core.App) {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("users collection: %v", err)
	}
	if existing, fErr := app.FindAuthRecordByEmail(col.Name, wbEmail); fErr == nil && existing != nil {
		existing.SetPassword(wbPassword)
		if sErr := app.Save(existing); sErr != nil {
			t.Fatalf("save existing user: %v", sErr)
		}
		return
	}
	rec := core.NewRecord(col)
	rec.SetEmail(wbEmail)
	rec.SetPassword(wbPassword)
	if err := app.Save(rec); err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

func login(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, baseURL+"/login",
		strings.NewReader(url.Values{"email": {wbEmail}, "password": {wbPassword}}.Encode()))
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

// mustReset rolls back bootstrap state on failure. Best-effort.
func mustReset(t *testing.T, app core.App) {
	t.Helper()
	if err := app.ClearBootstrap(); err != nil {
		t.Logf("ClearBootstrap: %v", err)
	}
}

// lowerPasswordCost drops the users password field's bcrypt cost to the
// minimum so password verification is not the single most expensive operation
// in the suite. Mirrors features/todo/fixture_test.go — the cost is per
// collection, so this is inert outside the test app.
func lowerPasswordCost(app core.App) error {
	col, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return err
	}
	pw, ok := col.Fields.GetByName("password").(*core.PasswordField)
	if !ok {
		return fmt.Errorf("users.password is %T, want *core.PasswordField", col.Fields.GetByName("password"))
	}
	pw.Cost = bcrypt.MinCost
	return app.Save(col)
}

// TestWhiteboard_ShapeBroadcastAndPersist is the end-to-end regression
// guard for the collaborative whiteboard:
//
//   - clientA draws a shape (POST op). The server merges it into the Loro
//     CRDT, persists the snapshot, and broadcasts the resolved shapes to
//     every OTHER client (BroadcastExcept, no echo to originator).
//   - clientB (a different SSE connection, same doc) MUST receive the
//     shapes event with the new shape.
//   - clientA MUST NOT receive its own shape echoed back — it already has
//     it optimistically in its local `shapes` array. This is the standard
//     CRDT "no echo" pattern (Yjs, Liveblocks, tldraw).
//   - The persister MUST hold the saved snapshot (PocketBase in prod).
//
// This proves the full architecture without a browser: SSE transport +
// CRDT convergence + persistence + no-echo broadcast to peers.
func TestWhiteboard_ShapeBroadcastAndPersist(t *testing.T) {
	t.Parallel()
	baseURL, persister, cleanup := webFixture(t)
	defer cleanup()

	jarA, errA := cookiejar.New(nil)
	if errA != nil {
		t.Fatalf("cookiejar A: %v", errA)
	}
	jarB, errB := cookiejar.New(nil)
	if errB != nil {
		t.Fatalf("cookiejar B: %v", errB)
	}
	clientA := &http.Client{
		Jar: jarA, Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	clientB := &http.Client{
		Jar: jarB, Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	login(t, clientA, baseURL)
	login(t, clientB, baseURL)

	docID := "doc-e2e-" + time.Now().Format("150405.000")

	streamA := openWBStream(t, clientA, baseURL, docID, "wbA")
	streamB := openWBStream(t, clientB, baseURL, docID, "wbB")
	defer streamA.close()
	defer streamB.close()
	streamA.settleJoin()
	streamB.settleJoin()

	// clientA creates a rectangle.
	op := collab.ShapeOp{Op: "add", Shape: collab.Shape{
		ID: "s1", Type: "rect", X: 10, Y: 20, W: 100, H: 50, Color: "#1f2937",
	}}
	body, mErr := json.Marshal(op)
	if mErr != nil {
		t.Fatalf("marshal op: %v", mErr)
	}
	resp, err := postWithClientID(context.Background(), clientA, baseURL+"/api/whiteboard/"+docID+"/update", "wbA", body)
	if err != nil {
		t.Fatalf("update POST: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update status = %d: %s", resp.StatusCode, readAll(resp))
	}
	resp.Body.Close()

	bEvents := streamB.waitFor(func(ev string) bool { return shapesEventContains([]string{ev}, "s1") })
	// The originator must NOT be echoed. An absence cannot be short-circuited,
	// so it keeps its full window — but only after the peer proved delivery
	// reached the hub, so it is provably delivered-or-not by then.
	aEvents := streamA.drain(100 * time.Millisecond)

	if !shapesEventContains(bEvents, "s1") {
		t.Fatalf("PEER (clientB) did not receive the shape broadcast.\nB events:\n%s", debugEvents(bEvents))
	}
	if shapesEventContains(aEvents, "s1") {
		t.Fatalf("ORIGINATOR (clientA) received its own shape echoed back "+
			"(BroadcastExcept is broken — CRDT no-echo standard).\nA events:\n%s", debugEvents(aEvents))
	}

	// Persistence: the in-memory persister must hold the snapshot for docID.
	if _, ok := persister.LoadSnapshot(docID); !ok {
		t.Fatalf("persister did not save snapshot for doc %s", docID)
	}

	// The worker's resolved shapes must include s1.
	if !shapeInList(whiteboardShapes(t, baseURL, clientB, docID), "s1") {
		t.Fatalf("resolved shapes from server do not include s1")
	}
}

// TestWhiteboard_PresenceBroadcast proves remote cursor presence is
// broadcast to peers (exclude-origin) so each client sees the others'
// cursors but not an echo of its own.
func TestWhiteboard_PresenceBroadcast(t *testing.T) {
	t.Parallel()
	baseURL, _, cleanup := webFixture(t)
	defer cleanup()

	jarA, errA := cookiejar.New(nil)
	if errA != nil {
		t.Fatalf("cookiejar A: %v", errA)
	}
	jarB, errB := cookiejar.New(nil)
	if errB != nil {
		t.Fatalf("cookiejar B: %v", errB)
	}
	clientA := &http.Client{
		Jar: jarA, Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	clientB := &http.Client{
		Jar: jarB, Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	login(t, clientA, baseURL)
	login(t, clientB, baseURL)

	docID := "doc-pres-" + time.Now().Format("150405.000")
	streamA := openWBStream(t, clientA, baseURL, docID, "wbA")
	streamB := openWBStream(t, clientB, baseURL, docID, "wbB")
	defer streamA.close()
	defer streamB.close()
	streamA.settleJoin()
	streamB.settleJoin()

	presence := collab.PresenceMsg{Type: "cursor", Doc: docID, User: "user-A", X: 0.5, Y: 0.5, TS: time.Now().UnixMilli()}
	pbody, mErr := json.Marshal(presence)
	if mErr != nil {
		t.Fatalf("marshal presence: %v", mErr)
	}
	resp, err := postWithClientID(context.Background(), clientA,
		baseURL+"/api/whiteboard/"+docID+"/presence", "wbA", pbody)
	if err != nil {
		t.Fatalf("presence POST: %v", err)
	}
	resp.Body.Close()

	bEvents := streamB.waitFor(func(ev string) bool { return presenceReceived([]string{ev}, "user-A") })
	aEvents := streamA.drain(100 * time.Millisecond) // absence check: no short-circuit

	if !presenceReceived(bEvents, "user-A") {
		t.Fatalf("PEER (clientB) did not receive cursor presence from user-A.\nB events:\n%s", debugEvents(bEvents))
	}
	if presenceReceived(aEvents, "user-A") {
		t.Fatalf("ORIGINATOR (clientA) received its own cursor echo "+
			"(exclude-origin broken).\nA events:\n%s", debugEvents(aEvents))
	}
}

// TestWhiteboard_OfflineReplay proves the server-side contract that makes
// the client's offline-first outbox safe: ops may arrive LATE (after the
// peer already connected and drew) and the server still merges each into
// the shared Loro CRDT, persists the resolved snapshot, and broadcasts to
// peers. This is what lets the browser buffer draws while offline and
// replay them on reconnect without losing or clobbering work.
//
// It mirrors the real flow: clientA draws + the op is held (simulated by
// delaying the POST), clientB draws in the meantime; then clientA's
// delayed op arrives and must converge on the server and reach clientB.
func TestWhiteboard_OfflineReplay(t *testing.T) {
	t.Parallel()
	baseURL, persister, cleanup := webFixture(t)
	defer cleanup()

	jarA, errA := cookiejar.New(nil)
	if errA != nil {
		t.Fatalf("cookiejar A: %v", errA)
	}
	jarB, errB := cookiejar.New(nil)
	if errB != nil {
		t.Fatalf("cookiejar B: %v", errB)
	}
	clientA := &http.Client{
		Jar: jarA, Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	clientB := &http.Client{
		Jar: jarB, Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	login(t, clientA, baseURL)
	login(t, clientB, baseURL)

	docID := "doc-offline-" + time.Now().Format("150405.000")
	streamA := openWBStream(t, clientA, baseURL, docID, "wbA")
	streamB := openWBStream(t, clientB, baseURL, docID, "wbB")
	defer streamA.close()
	defer streamB.close()
	streamA.settleJoin()
	streamB.settleJoin()

	// clientB draws immediately (online peer).
	bOp := collab.ShapeOp{Op: "add", Shape: collab.Shape{ID: "s-b", Type: "rect", X: 5, Y: 5, W: 40, H: 40, Color: "#000"}}
	bBody, mErr := json.Marshal(bOp)
	if mErr != nil {
		t.Fatalf("marshal bOp: %v", mErr)
	}
	resp, err := postWithClientID(context.Background(), clientB, baseURL+"/api/whiteboard/"+docID+"/update", "wbB", bBody)
	if err != nil {
		t.Fatalf("peer update POST: %v", err)
	}
	resp.Body.Close()

	// clientA "goes offline": its op is buffered, not POSTed yet.
	aOp := collab.ShapeOp{Op: "add", Shape: collab.Shape{
		ID: "s-a", Type: "ellipse", X: 60, Y: 60, W: 30, H: 30, Color: "#f00",
	}}
	aBody, mErr2 := json.Marshal(aOp)
	if mErr2 != nil {
		t.Fatalf("marshal aOp: %v", mErr2)
	}
	// The "offline window" is a correctness property here: the peer's op must
	// land BEFORE the replayed one so ordering is actually exercised. Wait for
	// B's shape to reach B's own stream, which proves it was merged and
	// broadcast — a stronger guarantee than sleeping 200ms and hoping.
	streamB.waitForEvent("s-b")

	// clientA "reconnects" and flushes its buffered op.
	updURL := baseURL + "/api/whiteboard/" + docID + "/update"
	respA, errRA := postWithClientID(context.Background(), clientA, updURL, "wbA", aBody)
	if errRA != nil {
		t.Fatalf("replay POST: %v", errRA)
	}
	respA.Body.Close()

	bEvents := streamB.waitFor(func(ev string) bool { return shapesEventContains([]string{ev}, "s-a") })
	if !shapesEventContains(bEvents, "s-a") {
		t.Fatalf("PEER did not receive the late (replayed) shape s-a.\nB events:\n%s", debugEvents(bEvents))
	}

	// Persistence must contain BOTH shapes (late op converged + saved).
	if _, ok := persister.LoadSnapshot(docID); !ok {
		t.Fatalf("persister did not save snapshot for %s", docID)
	}
	shapes := whiteboardShapes(t, baseURL, clientB, docID)
	if !shapeInList(shapes, "s-a") || !shapeInList(shapes, "s-b") {
		t.Fatalf("resolved shapes missing a replayed/peer shape: %+v", shapes)
	}
}
