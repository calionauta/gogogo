// SCOPE:plugin — Wire protocol unit tests (subjects, durables, nil-safety).
package natswire

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/nats"
)

func TestCrudSubjects(t *testing.T) {
	if CrudSubjectAll != "app.crud.todo.>" {
		t.Fatalf("CrudSubjectAll = %q, want the shared wildcard", CrudSubjectAll)
	}
	if CrudDurable("myapp") != "myapp-crud" {
		t.Fatalf("CrudDurable = %q, want the historical app durable", CrudDurable("myapp"))
	}
	if CrudDurable("myapp")+"-chi" != "myapp-crud-chi" {
		t.Fatal("chi durable convention broken")
	}
}

func TestNilPublisherIsNoOp(_ *testing.T) {
	var p *CrudPublisher
	p.Publish(CrudOpCreate, "u", &CrudOpData{ID: "x"}) // must not panic
	p.Close()                                          // idempotent, must not panic
	p.Close()
}

// TestCrudInstanceDurable pins the per-instance convention: two instances of
// one app never share a durable (the second subscription is otherwise
// rejected by the server and that instance silently stops converging).
func TestCrudInstanceDurable(t *testing.T) {
	a := CrudInstanceDurable("myapp", "prod-eu-1")
	b := CrudInstanceDurable("myapp", "prod-eu-2")
	if a == b {
		t.Fatalf("same durable for two instances: %q", a)
	}
	if a != "myapp-crud-prod-eu-1" {
		t.Fatalf("durable = %q, want myapp-crud-prod-eu-1", a)
	}
	if got := CrudInstanceDurable("myapp", ""); got != "myapp-crud-local" {
		t.Fatalf("empty instance durable = %q, want myapp-crud-local", got)
	}
}

// TestCrudPublisherForwardsOnCore pins the leaf-edge path: the same op goes
// out on plain NATS so a leaf node forwards it to the hub, while the local
// JetStream stream still captures it. Red-proof: drop the core publish and a
// hub subscriber never sees the message.
func TestCrudPublisherForwardsOnCore(t *testing.T) {
	h, err := nats.StartEmbeddedOn(t.TempDir(), "", "")
	if err != nil {
		t.Fatalf("embedded NATS: %v", err)
	}
	t.Cleanup(h.Close)

	sub, err := h.Conn.SubscribeSync("app.crud.todo.create")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if ferr := h.Conn.Flush(); ferr != nil {
		t.Fatalf("flush: %v", ferr)
	}
	p := NewCrudPublisher(h.JS, "edge")
	p.SetCore(h.Conn)
	p.Publish(CrudOpCreate, "user-1", &CrudOpData{ID: "todo-1", Title: "synced"})

	msg, err := sub.NextMsg(5 * time.Second)
	if err != nil {
		t.Fatalf("core message did not arrive: %v", err)
	}
	var payload CrudPayload
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if payload.UserID != "user-1" || payload.Data == nil || payload.Data.ID != "todo-1" {
		t.Fatalf("payload = %+v", payload)
	}
}
