// SCOPE:feature — Chi NATS bridge tests (red-proof for step 5).
//
// The bridge carries todo mutations across instances: publish on the same
// app.crud.todo.> subjects the PocketBase stack uses, apply to Turso on
// receipt, fan out to local streams. Tests boot a real embedded NATS
// server (ephemeral port) and a real migrated Turso file — no fakes.
package chi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/nats"
	"github.com/calionauta/gogogo/internal/queue"
)

// bridgeFixture boots Turso + embedded NATS + the consumer.
type bridgeFixture struct {
	tb  testing.TB
	q   *dbgen.Queries
	hub *queue.SSEHub
	pub *nats.CrudPublisher
}

func newBridgeFixture(tb testing.TB) *bridgeFixture {
	tb.Helper()

	sqlDB, err := db.OpenAt(tb.TempDir() + "/bridge.db")
	if err != nil {
		tb.Fatalf("db open: %v", err)
	}
	tb.Cleanup(func() { sqlDB.Close() })
	if merr := db.Migrate(sqlDB); merr != nil {
		tb.Fatalf("db migrate: %v", merr)
	}

	h, err := nats.StartEmbedded(tb.TempDir() + "/nats")
	if err != nil {
		tb.Fatalf("embedded nats: %v", err)
	}
	tb.Cleanup(h.Close)

	hub := queue.NewSSEHub()
	q := dbgen.New(sqlDB)
	// Identity first: applied ops reference limen_users(id), exactly as a
	// real signup would have created it. Fresh AUTOINCREMENT starts at 1.
	res, err := sqlDB.ExecContext(context.Background(),
		`INSERT INTO limen_users (email, created_at, updated_at) VALUES (?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		"bridge@example.com",
	)
	if err != nil {
		tb.Fatalf("bridge owner: %v", err)
	}
	if id, _ := res.LastInsertId(); id != 1 {
		tb.Fatalf("bridge owner id = %d, want 1", id)
	}
	// Publisher first: AddStream creates APP_CRUD, which the consumer's
	// Subscribe requires. Reversing this races "no stream matches subject"
	// and leaves a dead consumer (proven red by TestChiBridge*).
	pub := nats.NewCrudPublisher(h.JS)
	if pub == nil {
		tb.Fatal("crud publisher is nil (stream not created)")
	}
	ctx, cancel := context.WithCancel(context.Background())
	tb.Cleanup(cancel)
	go func() {
		_ = RunCrudConsumer(ctx, h.JS, "bridgetest", q, hub)
	}()
	return &bridgeFixture{tb: tb, q: q, hub: hub, pub: pub}
}

// waitRow polls Turso until the todo exists or the deadline hits.
func (f *bridgeFixture) waitRow(owner int64, id string) dbgen.Todo {
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

func TestChiBridgeCreateToggleDelete(t *testing.T) {
	f := newBridgeFixture(t)
	ctx := context.Background()

	// A chi client registered on the hub must hear the applied ops.
	streamCh := make(chan []byte, 16)
	f.hub.Register("bridge-client-1", "1", streamCh)
	defer f.hub.Unregister("bridge-client-1")

	f.pub.Publish(nats.CrudOpCreate, "1", &nats.CrudOpData{ID: "bridge-todo-1", Title: "From NATS"})
	got := f.waitRow(1, "bridge-todo-1")
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

	f.pub.Publish(nats.CrudOpToggle, "1", &nats.CrudOpData{ID: "bridge-todo-1", Completed: true})
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := f.q.GetTodo(ctx, dbgen.GetTodoParams{ID: "bridge-todo-1", Owner: 1})
		if err == nil && got.Completed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("toggle never applied (row=%+v err=%v)", got, err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	f.pub.Publish(nats.CrudOpDelete, "1", &nats.CrudOpData{ID: "bridge-todo-1"})
	deadline = time.Now().Add(5 * time.Second)
	for {
		if n, _ := f.q.CountTodosByOwner(ctx, 1); n == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("delete never applied")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestChiBridgeSkipsForeignIdentity(t *testing.T) {
	f := newBridgeFixture(t)

	// A PocketBase-world op carries a non-integer user id: the chi bridge
	// must skip it (different identity model) without dying — and stay
	// alive for the next valid op.
	f.pub.Publish(nats.CrudOpCreate, "pb-record-id", &nats.CrudOpData{ID: "bridge-foreign-1", Title: "PB"})
	time.Sleep(500 * time.Millisecond)
	if n, _ := f.q.CountTodosByOwner(context.Background(), 1); n != 0 {
		t.Fatalf("foreign op created %d rows, want 0", n)
	}

	f.pub.Publish(nats.CrudOpCreate, "1", &nats.CrudOpData{ID: "bridge-after-foreign", Title: "Survivor"})
	f.waitRow(1, "bridge-after-foreign")
}

// TestChiPublishToConsumerLoop is the step-5 end-to-end: an HTTP mutation on
// instance A publishes to NATS, and instance B's consumer applies it to its
// own Turso file. Both instances share one embedded server; each keeps its
// own database, mirroring two boxes (owner identity must exist on both —
func TestChiPublishToConsumerLoop(t *testing.T) {
	h, err := nats.StartEmbedded(t.TempDir() + "/nats-loop")
	if err != nil {
		t.Fatalf("embedded nats: %v", err)
	}
	t.Cleanup(h.Close)

	// Instance A: full HTTP stack (auth + todo with a live publisher).
	sqlA, err := db.OpenAt(t.TempDir() + "/loop-a.db")
	if err != nil {
		t.Fatalf("db open A: %v", err)
	}
	t.Cleanup(func() { sqlA.Close() })
	if merr := db.Migrate(sqlA); merr != nil {
		t.Fatalf("db migrate A: %v", merr)
	}
	authA, aerr := auth.New(sqlA, []byte("0123456789abcdef0123456789abcdef"), false)
	if aerr != nil {
		t.Fatalf("auth A: %v", aerr)
	}
	handlerA := NewHandler(dbgen.New(sqlA), authA, queue.NewSSEHub(), "daisyui")
	handlerA.SetPublisher(nats.NewCrudPublisher(h.JS))
	muxA := http.NewServeMux()
	muxA.Handle("/api/auth/", authA.Handler())
	muxA.Handle("/api/todos", handlerA.Routes())
	muxA.Handle("/api/todos/", handlerA.Routes())

	// Instance B: consumer only, on its own database.
	sqlB, err := db.OpenAt(t.TempDir() + "/loop-b.db")
	if err != nil {
		t.Fatalf("db open B: %v", err)
	}
	t.Cleanup(func() { sqlB.Close() })
	if err := db.Migrate(sqlB); err != nil {
		t.Fatalf("db migrate B: %v", err)
	}
	if _, err := sqlB.ExecContext(context.Background(),
		`INSERT INTO limen_users (email, created_at, updated_at) VALUES (?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		"loop@example.com",
	); err != nil {
		t.Fatalf("loop owner B: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		_ = RunCrudConsumer(ctx, h.JS, "looptest", dbgen.New(sqlB), queue.NewSSEHub())
	}()
	time.Sleep(300 * time.Millisecond) // let the consumer subscribe.

	// Signup on A, then mutate over HTTP.
	signupReq := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/auth/signup/credential",
		strings.NewReader(`{"email":"loop@example.com","password":"Correct-horse-99!"}`))
	signupReq.Header.Set("Content-Type", "application/json")
	signupRec := httptest.NewRecorder()
	muxA.ServeHTTP(signupRec, signupReq)
	if signupRec.Code != http.StatusOK {
		t.Fatalf("loop signup: status = %d (%s)", signupRec.Code, signupRec.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range signupRec.Result().Cookies() {
		if c.Name == auth.CookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("loop signup: no session cookie")
	}
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
	qB := dbgen.New(sqlB)
	deadline := time.Now().Add(8 * time.Second)
	for {
		list, err := qB.ListTodosByOwner(context.Background(), 1)
		if err == nil && len(list) == 1 && list[0].Title == "Loop item" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("instance B never converged (list=%+v err=%v)", list, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
