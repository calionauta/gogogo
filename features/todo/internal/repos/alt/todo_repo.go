// SCOPE:feature,removal=feature - Alt todo repo adapter (sqlc-backed, in-memory fallback)
package alt

import (
	"context"
	"errors"
	"fmt"
	"sync"

	todomodel "github.com/calionauta/gogogo/features/todo"
	"github.com/calionauta/gogogo/features/todo/internal/ports"
)

func NewTodoRepo() ports.TodoRepo {
	r, err := NewTodoRepoSQLC(":memory:")
	if err != nil {
		return &inmemRepo{items: make(map[string]todoItem)}
	}
	return r
}

type todoItem struct {
	Todo  todomodel.Todo
	Owner string
}

type inmemRepo struct {
	mu    sync.RWMutex
	items map[string]todoItem
}

func (r *inmemRepo) key(ownerID, id string) string {
	return ownerID + ":" + id
}

func (r *inmemRepo) List(_ context.Context, ownerID, filter string) ([]todomodel.Todo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	prefix := ownerID + ":"
	res := make([]todomodel.Todo, 0)
	for k, item := range r.items {
		if len(k) < len(prefix) || k[:len(prefix)] != prefix {
			continue
		}
		switch filter {
		case "active":
			if !item.Todo.Completed {
				res = append(res, item.Todo)
			}
		case "completed":
			if item.Todo.Completed {
				res = append(res, item.Todo)
			}
		case "all", "":
			res = append(res, item.Todo)
		default:
			res = append(res, item.Todo)
		}
	}
	return res, nil
}

func (r *inmemRepo) Create(_ context.Context, t todomodel.Todo, ownerID, idemKey string) (todomodel.Todo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t.ID = fmt.Sprintf("todo-%s-%s", idemKey, t.Title)
	r.items[r.key(ownerID, t.ID)] = todoItem{Todo: t, Owner: ownerID}
	return t, nil
}

func (r *inmemRepo) Get(_ context.Context, ownerID, id string) (todomodel.Todo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if item, ok := r.items[r.key(ownerID, id)]; ok {
		return item.Todo, nil
	}
	return todomodel.Todo{}, errors.New("not found")
}

func (r *inmemRepo) Update(_ context.Context, ownerID, id string, patch map[string]any) (todomodel.Todo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := r.key(ownerID, id)
	item, ok := r.items[k]
	if !ok {
		return todomodel.Todo{}, errors.New("not found")
	}
	if v, ok := patch["completed"]; ok {
		if b, ok := v.(bool); ok {
			item.Todo.Completed = b
		}
	}
	if v, ok := patch["title"]; ok {
		if s, ok := v.(string); ok {
			item.Todo.Title = s
		}
	}
	r.items[k] = item
	return item.Todo, nil
}

func (r *inmemRepo) Delete(ctx context.Context, ownerID, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.items, r.key(ownerID, id))
	return nil
}

func (r *inmemRepo) ClearCompleted(ctx context.Context, ownerID string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	prefix := ownerID + ":"
	n := 0
	for k, item := range r.items {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix && item.Todo.Completed {
			delete(r.items, k)
			n++
		}
	}
	return n, nil
}

func (r *inmemRepo) Count(ctx context.Context, ownerID string) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	prefix := ownerID + ":"
	n := 0
	for k := range r.items {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			n++
		}
	}
	return n, nil
}
