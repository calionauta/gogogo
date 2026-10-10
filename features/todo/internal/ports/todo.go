// SCOPE:feature,removal=feature - Todo ports (vertical slice)
package ports

import (
	"context"

	todomodel "github.com/calionauta/gogogo/features/todo"
)

// TodoRepo is the minimal repo contract for todo operations.
type TodoRepo interface {
	List(ctx context.Context, ownerID, filter string) ([]todomodel.Todo, error)
	Create(ctx context.Context, t todomodel.Todo, ownerID, idemKey string) (todomodel.Todo, error)
	Get(ctx context.Context, ownerID, id string) (todomodel.Todo, error)
	Update(ctx context.Context, ownerID, id string, patch map[string]any) (todomodel.Todo, error)
	Delete(ctx context.Context, ownerID, id string) error
	ClearCompleted(ctx context.Context, ownerID string) (int, error)
	Count(ctx context.Context, ownerID string) (int, error)
}
