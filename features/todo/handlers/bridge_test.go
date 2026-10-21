// SCOPE:feature — todo NATS bridge tests (red-proof for step 5).
//
// The bridge carries todo mutations across instances over the shared
// app.crud.todo.> subjects: publish, apply to Turso on receipt, fan out to
// local streams. Tests boot a real embedded NATS server (ephemeral port)
// and a real migrated Turso file — no fakes.
package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	natsio "github.com/nats-io/nats.go"

	"github.com/calionauta/gogogo/features/todo"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/nats"
	"github.com/calionauta/gogogo/internal/natswire"
	"github.com/calionauta/gogogo/internal/queue"
)

// bridgeFixture boots Turso + embedded NATS + the consumer.
type bridgeFixture struct {
	owner string
	tb    testing.TB
	q     *dbgen.Queries
	hub   *queue.SSEHub
	pub   *natswire.CrudPublisher
	js    natsio.JetStreamContext
}

func newBridgeFixture(tb testing.TB) *bridgeFixture {
	tb.Helper()

	sqlDB, err := db.OpenAt(tb.TempDir() + "/bridge.db")
	if err != nil {
		tb.Fatalf("db open: %v", err)
	}
	tb.Cleanup(func() { sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		tb.Fatalf("db migrate: %v", merr)
	}

	h, err := nats.StartEmbedded(tb.TempDir() + "/nats")
	if err != nil {
		tb.Fatalf("embedded nats: %v", err)
	}
	tb.Cleanup(h.Close)

	hub := queue.NewSSEHub()
	q := dbgen.New(sqlDB)
	// Identity first: applied ops reference users(id), exactly as a real
	// signup would have created it.
	owner := dbschema.NewID()
	if _, err := sqlDB.ExecContext(context.Background(),
		`INSERT INTO users (id, email, password_hash, name, created, updated) VALUES (?, ?, ?, ?, ?, ?)`,
		owner, "bridge@example.com", "hash", "Bridge", dbschema.Now(), dbschema.Now(),
	); err != nil {
		tb.Fatalf("bridge owner: %v", err)
	}
	// Publisher first: AddStream creates APP_CRUD, which the consumer's
	// Subscribe requires. Reversing this races "no stream matches subject"
	// and leaves a dead consumer (proven red by TestHandlersBridge*).
	// The fixture publishes as a REMOTE instance ("remote-1") while the
	// consumer runs as "local": the whole point of these tests is the
	// receiving side of the bridge.
	pub := natswire.NewCrudPublisher(h.JS, "remote-1")
	if pub == nil {
		tb.Fatal("crud publisher is nil (stream not created)")
	}
	ctx, cancel := context.WithCancel(context.Background())
	tb.Cleanup(cancel)
	go func() {
		_ = RunCrudConsumer(ctx, h.JS, "bridgetest", "local", q, nil, nil, todo.NewBus(hubSink{hub: hub}, "local"))
	}()
	return &bridgeFixture{tb: tb, q: q, hub: hub, pub: pub, owner: owner, js: h.JS}
}

// waitRow polls Turso until the todo exists or the deadline hits.
func (f *bridgeFixture) waitRow(owner string, id string) dbgen.Todo {
	f.tb.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if got, err := f.q.GetTodo(context.Background(), dbgen.GetTodoParams{ID: id, Owner: owner}); err == nil {
			return got
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.tb.Fatalf("todo %s never applied", id)
	return dbgen.Todo{}
}

func TestHandlersBridgeCreateToggleDelete(t *testing.T) {
	f := newBridgeFixture(t)
	ctx := context.Background()

	// A client registered on the hub must hear the applied ops.
	streamCh := make(chan []byte, 16)
	f.hub.Register("bridge-client-1", f.owner, streamCh)
	defer f.hub.Unregister("bridge-client-1")

	f.pub.Publish(natswire.CrudOpCreate, f.owner, &natswire.CrudOpData{ID: "bridge-todo-1", Title: "From NATS"})
	got := f.waitRow(f.owner, "bridge-todo-1")
	if got.Title != "From NATS" {
		t.Fatalf("applied title = %q, want From NATS", got.Title)
	}
	select {
	case payload := <-streamCh:
		var evt recordEvent
		if err := json.Unmarshal(payload, &evt); err != nil {
			t.Fatalf("hub payload decode: %v", err)
		}
		if evt.Event != "created" || evt.ID != "bridge-todo-1" {
			t.Fatalf("hub event = %+v, want created/bridge-todo-1", evt)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no hub fan-out for applied create")
	}

	f.pub.Publish(natswire.CrudOpToggle, f.owner, &natswire.CrudOpData{ID: "bridge-todo-1", Completed: true})
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := f.q.GetTodo(ctx, dbgen.GetTodoParams{ID: "bridge-todo-1", Owner: f.owner})
		if err == nil && got.Completed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("toggle never applied (row=%+v err=%v)", got, err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	f.pub.Publish(natswire.CrudOpDelete, f.owner, &natswire.CrudOpData{ID: "bridge-todo-1"})
	deadline = time.Now().Add(5 * time.Second)
	for {
		if n, _ := f.q.CountTodosByOwner(ctx, f.owner); n == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("delete never applied")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestHandlersBridgeSkipsForeignIdentity(t *testing.T) {
	f := newBridgeFixture(t)

	// An op for an unknown identity must skip without dying — and the
	// bridge must stay alive for the next valid op.
	f.pub.Publish(natswire.CrudOpCreate, "foreign-id",
		&natswire.CrudOpData{ID: "bridge-foreign-1", Title: "Foreign"})
	time.Sleep(500 * time.Millisecond)
	if n, _ := f.q.CountTodosByOwner(context.Background(), f.owner); n != 0 {
		t.Fatalf("foreign op created %d rows, want 0", n)
	}

	f.pub.Publish(natswire.CrudOpCreate, f.owner, &natswire.CrudOpData{ID: "bridge-after-foreign", Title: "Survivor"})
	f.waitRow(f.owner, "bridge-after-foreign")
}

// TestHandlersPublishToConsumerLoop is the step-5 end-to-end: an HTTP mutation on
// instance A publishes to NATS, and instance B's consumer applies it to its
// own Turso file. Both instances share one embedded server; each keeps its
// own database, mirroring two boxes. Owner identity must exist on both —
// the test mirrors it explicitly (production multi-box needs the shared
// remote Turso for that; the mechanism under test is publish→apply, not
// identity sync).
func TestHandlersPublishToConsumerLoop(t *testing.T) {
	h, err := nats.StartEmbedded(t.TempDir() + "/nats-loop")
	if err != nil {
		t.Fatalf("embedded nats: %v", err)
	}
	t.Cleanup(h.Close)

	muxA, sqlA := newLoopInstanceA(t, h)
	cookie := loopSignup(t, muxA)
	userA, err := dbgen.New(sqlA).GetUserByEmail(context.Background(), "loop@example.com")
	if err != nil {
		t.Fatalf("loop user A: %v", err)
	}
	_, qB := newLoopInstanceB(t, h.JS, userA.ID)

	// Mutate on A over HTTP.
	createReq := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/todos?filter=all", strings.NewReader("title=Loop+item"))
	createReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	createReq.AddCookie(cookie)
	createRec := httptest.NewRecorder()
	muxA.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("loop create: status = %d (%s)", createRec.Code, createRec.Body.String())
	}

	// Instance B converges without any HTTP call of its own.
	deadline := time.Now().Add(8 * time.Second)
	for {
		list, err := qB.ListTodosByOwner(context.Background(), userA.ID)
		if err == nil && len(list) == 1 && list[0].Title == "Loop item" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("instance B never converged (list=%+v err=%v)", list, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// newLoopInstanceA boots the full HTTP stack (auth + todo with a live
// publisher) on its own database. Split out so the loop test stays under
// the function-length gate.
func newLoopInstanceA(t *testing.T, h *nats.Handle) (http.Handler, *sql.DB) {
	t.Helper()
	sqlA, err := db.OpenAt(t.TempDir() + "/loop-a.db")
	if err != nil {
		t.Fatalf("db open A: %v", err)
	}
	t.Cleanup(func() { sqlA.Close() })
	if merr := dbschema.Migrate(sqlA); merr != nil {
		t.Fatalf("db migrate A: %v", merr)
	}
	authA, aerr := auth.New(dbgen.New(sqlA), false)
	if aerr != nil {
		t.Fatalf("auth A: %v", aerr)
	}
	handlerA := NewHandler(dbgen.New(sqlA), authA, queue.NewSSEHub(), "daisyui")
	handlerA.SetPublisher(natswire.NewCrudPublisher(h.JS, "remote-1"))
	muxA := http.NewServeMux()
	muxA.Handle("/api/auth/", authA.Routes())
	muxA.Handle("/api/todos", handlerA.Routes())
	muxA.Handle("/api/todos/", handlerA.Routes())
	return muxA, sqlA
}

// loopSignup signs up on muxA and returns the session cookie. Split out so
// the loop test stays under the function-length gate.
func loopSignup(t *testing.T, muxA http.Handler) *http.Cookie {
	t.Helper()
	signupReq := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/auth/signup/credential",
		strings.NewReader(`{"email":"loop@example.com","password":"Correct-horse-99!"}`))
	signupReq.Header.Set("Content-Type", "application/json")
	signupRec := httptest.NewRecorder()
	muxA.ServeHTTP(signupRec, signupReq)
	if signupRec.Code != http.StatusOK {
		t.Fatalf("loop signup: status = %d (%s)", signupRec.Code, signupRec.Body.String())
	}
	for _, c := range signupRec.Result().Cookies() {
		if c.Name == auth.CookieName {
			return c
		}
	}
	t.Fatal("loop signup: no session cookie")
	return nil
}

// newLoopInstanceB boots the consumer-only instance on its own database,
// with the loop user mirrored, and starts its consumer.
func newLoopInstanceB(t *testing.T, js natsio.JetStreamContext, userID string) (*sql.DB, *dbgen.Queries) {
	t.Helper()
	sqlB, err := db.OpenAt(t.TempDir() + "/loop-b.db")
	if err != nil {
		t.Fatalf("db open B: %v", err)
	}
	t.Cleanup(func() { sqlB.Close() })
	if merr := dbschema.Migrate(sqlB); merr != nil {
		t.Fatalf("db migrate B: %v", merr)
	}
	now := dbschema.Now()
	if _, err := sqlB.ExecContext(context.Background(),
		`INSERT INTO users (id, email, password_hash, name, created, updated) VALUES (?, ?, ?, ?, ?, ?)`,
		userID, "loop@example.com", "hash", "Loop", now, now,
	); err != nil {
		t.Fatalf("loop owner B: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		hub := queue.NewSSEHub()
		_ = RunCrudConsumer(ctx, js, "looptest", "loop-b", dbgen.New(sqlB), nil, nil,
			todo.NewBus(hubSink{hub: hub}, "loop-b"))
	}()
	time.Sleep(300 * time.Millisecond) // let the consumer subscribe.
	return sqlB, dbgen.New(sqlB)
}

// TestHandlersBridgeSkipsOwnOrigin pins the anti-duplication rule: the
// instance that published an op already wrote the row and fanned out, so its
// own consumer must not apply or broadcast it again. Before the origin tag,
// every local mutation produced TWO record events (direct hub + own echo).
func TestHandlersBridgeSkipsOwnOrigin(t *testing.T) {
	f := newBridgeFixture(t)
	ctx := context.Background()

	streamCh := make(chan []byte, 8)
	f.hub.Register("bridge-self-client", f.owner, streamCh)
	defer f.hub.Unregister("bridge-self-client")

	// Same origin as the consumer: must be ignored.
	own := natswire.NewCrudPublisher(f.js, "local")
	own.Publish(natswire.CrudOpCreate, f.owner, &natswire.CrudOpData{ID: "bridge-self-1", Title: "echo"})
	time.Sleep(500 * time.Millisecond)
	if _, err := f.q.GetTodo(ctx, dbgen.GetTodoParams{ID: "bridge-self-1", Owner: f.owner}); err == nil {
		t.Fatal("own-origin op was applied; the local DB already had it")
	}
	select {
	case payload := <-streamCh:
		t.Fatalf("own-origin op was broadcast again: %s", payload)
	default:
	}
}

// TestHandlersBridgeTwoInstancesConverge pins the multi-instance fix: each
// instance owns a durable, so BOTH receive a remote op; the origin instance
// ignores its own event and the remote one applies + fans out exactly once.
//
// RED-PROOFED: with the old shared durable the second consumer is rejected
// ("consumer is already bound") and never applies; with no origin tag the
// origin instance applies its own event and double-broadcasts.
func TestHandlersBridgeTwoInstancesConverge(t *testing.T) {
	h, err := nats.StartEmbedded(t.TempDir() + "/nats")
	if err != nil {
		t.Fatalf("embedded nats: %v", err)
	}
	t.Cleanup(h.Close)
	pub := natswire.NewCrudPublisher(h.JS, "inst-a")
	if pub == nil {
		t.Fatal("crud publisher is nil (stream not created)")
	}

	owner := dbschema.NewID()
	now := dbschema.Now()
	instance := func(name string) (*dbgen.Queries, *queue.SSEHub) {
		sqlDB, oerr := db.OpenAt(t.TempDir() + "/" + name + ".db")
		if oerr != nil {
			t.Fatalf("db open %s: %v", name, oerr)
		}
		t.Cleanup(func() { sqlDB.Close() })
		if merr := dbschema.Migrate(sqlDB); merr != nil {
			t.Fatalf("db migrate %s: %v", name, merr)
		}
		if _, xerr := sqlDB.ExecContext(context.Background(),
			`INSERT INTO users (id, email, password_hash, name, created, updated) VALUES (?, ?, ?, ?, ?, ?)`,
			owner, name+"@example.com", "hash", name, now, now,
		); xerr != nil {
			t.Fatalf("%s owner: %v", name, xerr)
		}
		hub := queue.NewSSEHub()
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		go func() {
			_ = RunCrudConsumer(ctx, h.JS, "multitest", name, dbgen.New(sqlDB), nil, nil, todo.NewBus(hubSink{hub: hub}, name))
		}()
		return dbgen.New(sqlDB), hub
	}
	qA, hubA := instance("inst-a")
	qB, hubB := instance("inst-b")
	time.Sleep(400 * time.Millisecond) // let both consumers bind (separate durables)

	chA := make(chan []byte, 8)
	chB := make(chan []byte, 8)
	hubA.Register("a-client", owner, chA)
	hubB.Register("b-client", owner, chB)

	pub.Publish(natswire.CrudOpCreate, owner, &natswire.CrudOpData{ID: "multi-1", Title: "from a"})

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := qB.GetTodo(context.Background(), dbgen.GetTodoParams{ID: "multi-1", Owner: owner}); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("instance B never applied the remote op (durable collision or dead consumer)")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// A must NOT have applied its own op.
	time.Sleep(300 * time.Millisecond)
	if _, err := qA.GetTodo(context.Background(), dbgen.GetTodoParams{ID: "multi-1", Owner: owner}); err == nil {
		t.Fatal("origin instance applied its own op (origin tag ignored)")
	}
	select {
	case payload := <-chB:
		var evt recordEvent
		if err := json.Unmarshal(payload, &evt); err != nil || evt.Event != "created" {
			t.Fatalf("B hub payload = %s", payload)
		}
	default:
		t.Fatal("instance B did not fan out the applied op")
	}
	select {
	case payload := <-chA:
		t.Fatalf("origin instance broadcast its own op: %s", payload)
	case <-time.After(200 * time.Millisecond):
	}
}
