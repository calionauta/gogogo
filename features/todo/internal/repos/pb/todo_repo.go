// SCOPE:feature,removal=feature - PB-backed todo repo adapter (vertical slice)
package pb

import (
	"context"

	"github.com/pocketbase/pocketbase/core"

	"github.com/calionauta/gogogo/features/store"
	"github.com/calionauta/gogogo/features/store/pbstore"
	todomodel "github.com/calionauta/gogogo/features/todo"
	"github.com/calionauta/gogogo/features/todo/internal/ports"
)

type TodoRepo struct {
	store store.EntityStore[todomodel.Todo]
}

func NewTodoRepo(app core.App) *TodoRepo {
	return &TodoRepo{store: pbstore.New(app, "todos")}
}

func (r *TodoRepo) List(ctx context.Context, ownerID, filter string) ([]todomodel.Todo, error) {
	return r.store.List(ctx, ownerID, filter)
}

func (r *TodoRepo) Create(ctx context.Context, t todomodel.Todo, ownerID, idemKey string) (todomodel.Todo, error) {
	return r.store.Create(ctx, t, ownerID, idemKey)
}

func (r *TodoRepo) Get(ctx context.Context, ownerID, id string) (todomodel.Todo, error) {
	return r.store.Get(ctx, ownerID, id)
}

func (r *TodoRepo) Update(ctx context.Context, ownerID, id string, patch map[string]any) (todomodel.Todo, error) {
	return r.store.Update(ctx, ownerID, id, patch)
}

func (r *TodoRepo) Delete(ctx context.Context, ownerID, id string) error {
	return r.store.Delete(ctx, ownerID, id)
}

func (r *TodoRepo) ClearCompleted(ctx context.Context, ownerID string) (int, error) {
	return r.store.ClearCompleted(ctx, ownerID)
}

func (r *TodoRepo) Count(ctx context.Context, ownerID string) (int, error) {
	return r.store.Count(ctx, ownerID)
}

var _ ports.TodoRepo = (*TodoRepo)(nil)
