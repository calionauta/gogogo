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
