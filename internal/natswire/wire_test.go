// SCOPE:plugin — Wire protocol unit tests (subjects, durables, nil-safety).
package natswire

import "testing"

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
