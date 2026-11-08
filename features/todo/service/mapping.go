// Transport-free todo domain service — one rule, every frontend.
package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/calionauta/gogogo/features/todo"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/rbac"
)

// --- identity / mapping (single home) ------------------------------------

func CanDeleteRow(role, requester string, item todo.Todo) bool {
	if requester != "" && requester == item.Owner {
		return true
	}
	if rbac.IsViewer(role) {
		return false
	}
	return rbac.IsAdmin(role)
}

func ToTodo(row dbgen.ExtTodoTodo) (todo.Todo, error) {
	created, err := time.Parse(dbschema.TimeFormat, row.Created)
	if err != nil {
		return todo.Todo{}, fmt.Errorf("todo %s created %q: %w", row.ID, row.Created, err)
	}
	updated, err := time.Parse(dbschema.TimeFormat, row.Updated)
	if err != nil {
		return todo.Todo{}, fmt.Errorf("todo %s updated %q: %w", row.ID, row.Updated, err)
	}
	return todo.Todo{
		ID:         row.ID,
		Title:      row.Title,
		Completed:  row.Completed,
		CreatedAt:  created,
		UpdatedAt:  updated,
		Owner:      row.Owner,
		Visibility: row.Visibility,
	}, nil
}

func ToVisibleTodo(row dbgen.ListVisibleTodosRow) (todo.Todo, error) {
	item, err := ToTodo(dbgen.ExtTodoTodo{
		ID: row.ID, Owner: row.Owner, Title: row.Title,
		Completed: row.Completed, IdemKey: row.IdemKey,
		Visibility: row.Visibility, Created: row.Created, Updated: row.Updated,
	})
	if err != nil {
		return todo.Todo{}, err
	}
	item.OwnerEmail = row.OwnerEmail
	return item, nil
}

func ToVisibleOne(row dbgen.GetTodoVisibleRow) (todo.Todo, error) {
	item, err := ToTodo(dbgen.ExtTodoTodo{
		ID: row.ID, Owner: row.Owner, Title: row.Title,
		Completed: row.Completed, IdemKey: row.IdemKey,
		Visibility: row.Visibility, Created: row.Created, Updated: row.Updated,
	})
	if err != nil {
		return todo.Todo{}, err
	}
	item.OwnerEmail = row.OwnerEmail
	return item, nil
}

func ValidIdemKey(key string) bool {
	if key == "" || len(key) > 128 {
		return false
	}
	for _, r := range key {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_'
		if !ok {
			return false
		}
	}
	return true
}

// --- indexed writes -------------------------------------------------------

func (s *Service) indexEnabled() bool { return s.Index != nil && s.DB != nil }

// CreateIndexed / DeleteIndexed / ClearCompletedIndexed are the raw
// indexed write paths for callers (bridge, tests) that drive the store
// directly without the Create/Toggle/Delete domain rules.
func (s *Service) CreateIndexed(ctx context.Context, p dbgen.CreateTodoParams) error {
	return s.createIndexed(ctx, p)
}

func (s *Service) DeleteIndexed(ctx context.Context, owner, id string) error {
	return s.deleteIndexed(ctx, owner, id)
}

func (s *Service) ClearCompletedIndexed(ctx context.Context, owner string) (int64, error) {
	return s.clearIndexed(ctx, owner)
}

func (s *Service) createIndexed(ctx context.Context, p dbgen.CreateTodoParams) error {
	if !s.indexEnabled() {
		_, err := s.Q.CreateTodo(ctx, p)
		return err
	}
	return dbschema.Transact(ctx, s.DB, "todo create+index", func(ctx context.Context, tx *sql.Tx) error {
		if _, err := s.Q.WithTx(tx).CreateTodo(ctx, p); err != nil {
			return err
		}
		return s.Index.IndexTodo(ctx, tx, p.Owner, p.ID, p.Title, p.Visibility)
	})
}

func (s *Service) deleteIndexed(ctx context.Context, owner, id string) error {
	if !s.indexEnabled() {
		return s.Q.DeleteTodoByID(ctx, id)
	}
	return dbschema.Transact(ctx, s.DB, "todo delete+index", func(ctx context.Context, tx *sql.Tx) error {
		if err := s.Index.DeleteTodo(ctx, tx, owner, id); err != nil {
			return err
		}
		return s.Q.WithTx(tx).DeleteTodoByID(ctx, id)
	})
}

func (s *Service) clearIndexed(ctx context.Context, owner string) (int64, error) {
	if !s.indexEnabled() {
		return s.Q.DeleteCompletedByOwner(ctx, owner)
	}
	var count int64
	err := dbschema.Transact(ctx, s.DB, "todo clear+index", func(ctx context.Context, tx *sql.Tx) error {
		txq := s.Q.WithTx(tx)
		ids, err := txq.ListCompletedIDsByOwner(ctx, owner)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if derr := s.Index.DeleteTodo(ctx, tx, owner, id); derr != nil {
				return derr
			}
		}
		count, err = txq.DeleteCompletedByOwner(ctx, owner)
		return err
	})
	return count, err
}
