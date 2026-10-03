// SCOPE:layer=feature,removal=feature — Unit tests for the RequireOwner choke point.
package handlers

import (
	"errors"
	"testing"

	"github.com/pocketbase/pocketbase/core"

	"github.com/calionauta/gogogo-fullstack-template/features/todo"
)

// A nil event or an event without auth must fail closed with
// ErrNoOwner — never an empty owner string that the store would treat
// as "no filter". The authenticated round-trip (Auth set → record id)
// is covered at the HTTP level in features/todo/owner_require_test.go,
// which logs in for real and asserts tenant isolation end to end.
func TestRequireOwnerRejectsNilEvent(t *testing.T) {
	if _, err := RequireOwner(nil); !errors.Is(err, ErrNoOwner) {
		t.Errorf("RequireOwner(nil) = %v, want ErrNoOwner", err)
	}
}

func TestRequireOwnerRejectsUnauthenticatedEvent(t *testing.T) {
	c := &core.RequestEvent{}
	if _, err := RequireOwner(c); !errors.Is(err, ErrNoOwner) {
		t.Errorf("RequireOwner(no auth) = %v, want ErrNoOwner", err)
	}
}

// The repository helpers are the second line of defense: even a
// handler that forgot its RequireOwner check fails closed instead of
// touching the store unscoped. A zero-value handler never reaches the
// store here — the owner check runs first — so these need no app.
func TestListTodosRejectsUnauthenticated(t *testing.T) {
	var h TodoHandler
	if _, err := h.listTodos(nil, "all"); !errors.Is(err, ErrNoOwner) {
		t.Errorf("listTodos(nil) = %v, want ErrNoOwner", err)
	}
}

func TestCountOwnedTodosRejectsUnauthenticated(t *testing.T) {
	var h TodoHandler
	if _, err := h.countOwnedTodos(nil); !errors.Is(err, ErrNoOwner) {
		t.Errorf("countOwnedTodos(nil) = %v, want ErrNoOwner", err)
	}
}

func TestSaveTodoRejectsEmptyOwner(t *testing.T) {
	var h TodoHandler
	item := todo.Todo{Title: "ownerless"}
	if err := h.saveTodo(nil, &item, "", "idem-key"); !errors.Is(err, ErrNoOwner) {
		t.Errorf("saveTodo(empty owner) = %v, want ErrNoOwner", err)
	}
	if item.ID != "" {
		t.Errorf("rejected save mutated the item (ID=%q)", item.ID)
	}
}
